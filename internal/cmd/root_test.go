// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"strings"
	"testing"
)

// TestRunRejectsInvalidPathStyle verifies that malformed boolean configuration is not silently disabled.
func TestRunRejectsInvalidPathStyle(t *testing.T) {
	t.Setenv("REGISTRY_S3_PATH_STYLE", "tru")

	err := Run(nil)
	if err == nil || !strings.Contains(err.Error(), "parse REGISTRY_S3_PATH_STYLE") {
		t.Fatalf("Run() error = %v, want REGISTRY_S3_PATH_STYLE parse error", err)
	}
}
