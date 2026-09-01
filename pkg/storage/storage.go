// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"io"
	"io/fs"
)

// ErrNotFound identifies a missing object.
var ErrNotFound = fs.ErrNotExist

// Range identifies an inclusive byte range.
type Range struct {
	Start int64
	End   int64
}

// Reader is the narrow read-only contract used by the registry handler.
type Reader interface {
	// Size returns an object's size without reading its contents.
	Size(context.Context, string) (int64, error)
	// Open opens a full object when the range is nil or one inclusive range.
	// It returns the length of the opened stream.
	Open(context.Context, string, *Range) (io.ReadCloser, int64, error)
}
