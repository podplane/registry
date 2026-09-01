// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const bucket = "registry-e2e"

// manifest contains the image objects required by the pull test.
type manifest struct {
	Config descriptor   `json:"config"`
	Layers []descriptor `json:"layers"`
}

// descriptor identifies one content-addressed image object.
type descriptor struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
}

// TestPull publishes an ocimage layout to SeaweedFS and pulls it through Registry.
func TestPull(t *testing.T) {
	if os.Getenv("REGISTRY_E2E") == "" {
		t.Skip("set REGISTRY_E2E=1 to run the end-to-end test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	endpoint := "http://127.0.0.1:" + requiredEnv(t, "REGISTRY_E2E_S3_PORT")
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("load AWS configuration: %v", err)
	}
	objects := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	if _, err = objects.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	publishLayout(t, ctx, objects, requiredEnv(t, "REGISTRY_E2E_LAYOUT"), "e2e/hello")

	base := "http://127.0.0.1:" + requiredEnv(t, "REGISTRY_E2E_REGISTRY_PORT")
	response, body := request(t, http.MethodGet, base+"/v2/", "", "")
	if response.StatusCode != http.StatusOK || len(body) != 0 {
		t.Fatalf("GET /v2/ = %d %q, want 200 with an empty body", response.StatusCode, body)
	}
	response, body = request(t, http.MethodGet, base+"/v2/e2e/hello/manifests/v1", "Accept", "application/vnd.oci.image.manifest.v1+json")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("pull manifest = %d %q, want 200", response.StatusCode, body)
	}
	var image manifest
	if err = json.Unmarshal(body, &image); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if image.Config.Digest == "" || len(image.Layers) != 1 {
		t.Fatalf("manifest config = %q, layers = %d, want one complete image", image.Config.Digest, len(image.Layers))
	}

	response, configBody := request(t, http.MethodGet, blobURL(base, image.Config.Digest), "", "")
	if response.StatusCode != http.StatusOK || !json.Valid(configBody) {
		t.Fatalf("pull config = %d %q, want valid JSON", response.StatusCode, configBody)
	}
	response, layer := request(t, http.MethodGet, blobURL(base, image.Layers[0].Digest), "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("pull layer = %d %q, want 200", response.StatusCode, layer)
	}
	verifyLayer(t, layer)

	response, body = request(t, http.MethodHead, blobURL(base, image.Layers[0].Digest), "", "")
	if response.StatusCode != http.StatusOK || len(body) != 0 || response.ContentLength != int64(len(layer)) {
		t.Fatalf("HEAD layer = status %d, body %d bytes, length %d; want 200, empty, %d", response.StatusCode, len(body), response.ContentLength, len(layer))
	}
	response, body = request(t, http.MethodGet, blobURL(base, image.Layers[0].Digest), "Range", "bytes=0-3")
	if response.StatusCode != http.StatusPartialContent || !bytes.Equal(body, layer[:4]) {
		t.Fatalf("range layer = status %d, body %x; want 206 and %x", response.StatusCode, body, layer[:4])
	}
}

// publishLayout uploads immutable layout objects before publishing index.json.
func publishLayout(t *testing.T, ctx context.Context, objects *s3.Client, layout, prefix string) {
	t.Helper()
	var indexPath string
	err := filepath.WalkDir(layout, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(layout, path)
		if err != nil {
			return err
		}
		if relative == "index.json" {
			indexPath = path
			return nil
		}
		return putFile(ctx, objects, path, prefix+"/"+filepath.ToSlash(relative))
	})
	if err != nil {
		t.Fatalf("upload layout: %v", err)
	}
	if indexPath == "" {
		t.Fatal("layout has no index.json")
	}
	if err = putFile(ctx, objects, indexPath, prefix+"/index.json"); err != nil {
		t.Fatalf("publish index: %v", err)
	}
}

// putFile uploads one local file to the test bucket.
func putFile(ctx context.Context, objects *s3.Client, path, key string) error {
	body, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	info, err := body.Stat()
	if err != nil {
		return err
	}
	_, err = objects.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: body, ContentLength: aws.Int64(info.Size())})
	return err
}

// request performs one registry request and returns its complete response body.
func request(t *testing.T, method, url, header, value string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if header != "" {
		req.Header.Set(header, value)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return response, body
}

// blobURL returns the Distribution API URL for one digest.
func blobURL(base, digest string) string {
	return base + "/v2/e2e/hello/blobs/" + digest
}

// verifyLayer confirms the pulled tar layer contains the expected payload.
func verifyLayer(t *testing.T, compressed []byte) {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("open layer: %v", err)
	}
	defer func() { _ = reader.Close() }()
	archive := tar.NewReader(reader)
	for {
		header, nextErr := archive.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatalf("read layer: %v", nextErr)
		}
		if strings.TrimPrefix(header.Name, "./") != "hello.txt" {
			continue
		}
		body, readErr := io.ReadAll(archive)
		if readErr != nil {
			t.Fatalf("read hello.txt: %v", readErr)
		}
		if string(body) != "hello from the read-only registry\n" {
			t.Fatalf("hello.txt = %q", body)
		}
		return
	}
	t.Fatal("pulled layer has no hello.txt")
}

// requiredEnv returns a required end-to-end test setting.
func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}
