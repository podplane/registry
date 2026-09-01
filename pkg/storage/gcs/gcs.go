// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"

	cloudstorage "cloud.google.com/go/storage"
	"github.com/podplane/registry/pkg/storage"
	"google.golang.org/api/googleapi"
)

// store provides storage operations for one GCS bucket.
type store struct {
	bucket *cloudstorage.BucketHandle
}

// New returns storage backed by one GCS bucket without accessing it.
func New(client *cloudstorage.Client, bucket string) storage.Reader {
	return &store{bucket: client.Bucket(bucket)}
}

// Size returns the size of one GCS object.
func (s *store) Size(ctx context.Context, key string) (int64, error) {
	attrs, err := s.bucket.Object(key).Attrs(ctx)
	if err != nil {
		return 0, mapError(err)
	}
	return attrs.Size, nil
}

// Open opens a full or ranged GCS object without decompressive transcoding.
func (s *store) Open(ctx context.Context, key string, requested *storage.Range) (io.ReadCloser, int64, error) {
	object := s.bucket.Object(key).ReadCompressed(true)
	var (
		reader *cloudstorage.Reader
		err    error
	)
	if requested == nil {
		reader, err = object.NewReader(ctx)
	} else {
		reader, err = object.NewRangeReader(ctx, requested.Start, requested.End-requested.Start+1)
	}
	if err != nil {
		return nil, 0, mapError(err)
	}
	length := reader.Attrs.Size
	if requested != nil {
		length = requested.End - requested.Start + 1
	}
	return reader, length, nil
}

// mapError converts GCS errors into provider-neutral storage errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, cloudstorage.ErrObjectNotExist) {
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}
	var apiError *googleapi.Error
	if errors.As(err, &apiError) && apiError.Code == 404 {
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}
	return err
}
