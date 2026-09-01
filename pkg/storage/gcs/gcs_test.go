// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package gcs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	cloudstorage "cloud.google.com/go/storage"
	"github.com/podplane/registry/pkg/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip executes one synthetic GCS HTTP request.
func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// TestReadsUseGCSObjectRequests verifies metadata and ranged GCS reads.
func TestReadsUseGCSObjectRequests(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "application/octet-stream")
		header.Set("X-Goog-Generation", "42")
		status, body := http.StatusOK, `{"kind":"storage#object","bucket":"bucket","name":"repo/blob","size":"10","generation":"42","metageneration":"1","etag":"etag"}`
		if request.URL.Query().Get("alt") == "media" || !strings.Contains(request.URL.Path, "/storage/v1/") {
			body = "0123456789"
			if request.Header.Get("Range") == "bytes=2-5" {
				status, body = http.StatusPartialContent, "2345"
				header.Set("Content-Range", "bytes 2-5/10")
			}
		}
		header.Set("Content-Length", strconv.Itoa(len(body)))
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	client, err := cloudstorage.NewClient(context.Background(), option.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	store := New(client, "bucket")
	size, err := store.Size(context.Background(), "repo/blob")
	if err != nil || size != 10 {
		t.Fatalf("Size() = %d, %v", size, err)
	}
	reader, length, err := store.Open(context.Background(), "repo/blob", &storage.Range{Start: 2, End: 5})
	if err != nil || length != 4 {
		t.Fatalf("Open() length = %d, error = %v", length, err)
	}
	ranged, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil || string(ranged) != "2345" {
		t.Fatalf("Open() = %q, %v", ranged, err)
	}
}

// TestErrorMapping verifies provider-neutral GCS error mapping.
func TestErrorMapping(t *testing.T) {
	if err := mapError(cloudstorage.ErrObjectNotExist); !errors.Is(err, storage.ErrNotFound) || !errors.Is(err, cloudstorage.ErrObjectNotExist) {
		t.Fatalf("not found error = %v", err)
	}
	if err := mapError(&googleapi.Error{Code: 404}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("HTTP not found error = %v", err)
	}
}
