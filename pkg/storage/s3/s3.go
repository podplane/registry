// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/podplane/registry/pkg/storage"
)

// Client is the subset of the AWS SDK v2 S3 client used by the reader.
type Client interface {
	// HeadObject returns object metadata.
	HeadObject(context.Context, *awss3.HeadObjectInput, ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error)
	// GetObject opens an object's contents.
	GetObject(context.Context, *awss3.GetObjectInput, ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
}

// store provides storage operations for one S3 bucket.
type store struct {
	client Client
	bucket string
}

// New returns storage backed by one S3 bucket without accessing it.
func New(client Client, bucket string) storage.Reader {
	return &store{client: client, bucket: bucket}
}

// Size returns the size of one S3 object.
func (s *store) Size(ctx context.Context, key string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return 0, mapError(err)
	}
	return aws.ToInt64(out.ContentLength), nil
}

// Open opens a full or ranged S3 object.
func (s *store) Open(ctx context.Context, key string, requested *storage.Range) (io.ReadCloser, int64, error) {
	in := &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if requested != nil {
		in.Range = aws.String(fmt.Sprintf("bytes=%d-%d", requested.Start, requested.End))
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		return nil, 0, mapError(err)
	}
	return out.Body, aws.ToInt64(out.ContentLength), nil
}

// mapError converts S3 errors into provider-neutral storage errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var missing *types.NoSuchKey
	if errors.As(err, &missing) {
		return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket":
			return fmt.Errorf("%w: %w", storage.ErrNotFound, err)
		}
	}
	return err
}
