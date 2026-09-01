// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package buildvars

// Build metadata is set at build time.
var (
	buildVersion = "dev"
	buildDate    = "unknown"
	commitHash   = "unknown"
	commitDate   = "unknown"
	commitBranch = "unknown"
)

// BuildVersion returns the immutable build version.
func BuildVersion() string {
	return buildVersion
}

// BuildDate returns the immutable build date.
func BuildDate() string {
	return buildDate
}

// CommitHash returns the immutable Git commit hash.
func CommitHash() string {
	return commitHash
}

// CommitDate returns the immutable Git commit date.
func CommitDate() string {
	return commitDate
}

// CommitBranch returns the immutable Git commit branch.
func CommitBranch() string {
	return commitBranch
}
