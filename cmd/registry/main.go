// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"os"

	"github.com/podplane/registry/internal/cmd"
)

// main runs the standalone registry command.
func main() {
	if err := cmd.Run(os.Args[1:]); err != nil {
		slog.Error("registry stopped", "error", err)
		os.Exit(1)
	}
}
