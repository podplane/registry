// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package s3_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/podplane/registry/pkg/storage"
	registrys3 "github.com/podplane/registry/pkg/storage/s3"
)

// fakeClient records AWS SDK requests.
type fakeClient struct {
	head *awss3.HeadObjectInput
	get  *awss3.GetObjectInput
	err  error
}

// HeadObject records an S3 metadata request.
func (c *fakeClient) HeadObject(_ context.Context, input *awss3.HeadObjectInput, _ ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	c.head = input
	if c.err != nil {
		return nil, c.err
	}
	size := int64(10)
	return &awss3.HeadObjectOutput{ContentLength: &size}, nil
}

// GetObject records an S3 object read.
func (c *fakeClient) GetObject(_ context.Context, input *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	c.get = input
	if c.err != nil {
		return nil, c.err
	}
	size := int64(4)
	return &awss3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("data")), ContentLength: &size}, nil
}

// TestNotFoundMapping verifies that S3 not-found errors retain both contracts.
func TestNotFoundMapping(t *testing.T) {
	missing := &types.NoSuchKey{}
	store := registrys3.New(&fakeClient{err: missing}, "bucket")
	_, err := store.Size(context.Background(), "missing")
	if !errors.Is(err, storage.ErrNotFound) || !errors.Is(err, missing) {
		t.Fatalf("Size() error = %v", err)
	}
}

// TestRequestMapping verifies metadata and ranged-read S3 request mapping.
func TestRequestMapping(t *testing.T) {
	client := &fakeClient{}
	store := registrys3.New(client, "bucket")
	if size, err := store.Size(context.Background(), "repo/index.json"); err != nil || size != 10 {
		t.Fatalf("Size() = %d, %v", size, err)
	}
	body, size, err := store.Open(context.Background(), "repo/blob", &storage.Range{Start: 2, End: 5})
	if err != nil || size != 4 {
		t.Fatalf("Open() size = %d, error = %v", size, err)
	}
	data, err := io.ReadAll(body)
	if closeErr := body.Close(); err == nil {
		err = closeErr
	}
	if err != nil || string(data) != "data" {
		t.Fatalf("Open() = %q, %v", data, err)
	}
	if *client.head.Bucket != "bucket" || *client.head.Key != "repo/index.json" {
		t.Fatalf("head input = %#v", client.head)
	}
	if *client.get.Key != "repo/blob" || *client.get.Range != "bytes=2-5" {
		t.Fatalf("get input = %#v", client.get)
	}
}
