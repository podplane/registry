# Podplane <https://podplane.dev>
# Copyright The Podplane Authors
# SPDX-License-Identifier: Apache-2.0

.DEFAULT_GOAL := help

BUILDVARS := github.com/podplane/registry/internal/buildvars
BUILD_VERSION ?= dev
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT_HASH ?= $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)
COMMIT_DATE ?= $(shell TZ=UTC git show -s --date=format-local:'%Y-%m-%dT%H:%M:%SZ' --format=%cd HEAD 2>/dev/null || echo unknown)
COMMIT_BRANCH ?= $(shell branch=$$(git branch --show-current 2>/dev/null); if [ -n "$$branch" ]; then echo "$$branch"; else echo unknown; fi)
LDFLAGS := -X $(BUILDVARS).buildVersion=$(BUILD_VERSION) -X $(BUILDVARS).buildDate=$(BUILD_DATE) -X $(BUILDVARS).commitHash=$(COMMIT_HASH) -X $(BUILDVARS).commitDate=$(COMMIT_DATE) -X $(BUILDVARS).commitBranch=$(COMMIT_BRANCH)

.PHONY: help setup generate check-generated fmt precommit lint test e2e build clean
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; print "Targets:"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Verify development tools and install Git hooks
	@go version
	@"$$(go -C tools tool -n overmind)" --version
	@tmux -V
	@"$$(go -C tools tool -n ocimage)" version
	@weed version
	@shellcheck --version >/dev/null
	@mkdir -p .git/hooks
	@cp scripts/git-hooks/pre-commit .git/hooks/pre-commit
	@cp scripts/git-hooks/commit-msg .git/hooks/commit-msg
	@chmod +x .git/hooks/pre-commit .git/hooks/commit-msg

generate: ## Regenerate module metadata
	go mod tidy
	go -C tools mod tidy

check-generated: ## Check generated module metadata
	@before=$$(cksum go.mod go.sum tools/go.mod tools/go.sum); \
	$(MAKE) --no-print-directory generate >/dev/null; \
	after=$$(cksum go.mod go.sum tools/go.mod tools/go.sum); \
	if [ "$$before" != "$$after" ]; then echo "generated module metadata was stale"; exit 1; fi

fmt: ## Format Go source
	gofmt -w $$(find . -name '*.go' -not -path './bin/*')

precommit: ## Run fast read-only checks
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './bin/*'))"
	go vet ./...

lint: ## Run static analysis
	"$$(go -C tools tool -n golangci-lint)" run
	shellcheck scripts/*.sh scripts/git-hooks/*

test: ## Run tests with race detection
	go test -race ./...

e2e: build ## Run the local SeaweedFS registry pull test
	OCIMAGE_BIN="$$(go -C tools tool -n ocimage)" OVERMIND_BIN="$$(go -C tools tool -n overmind)" ./scripts/e2e.sh

build: ## Build the standalone registry
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/registry ./cmd/registry

clean: ## Remove local build artifacts
	rm -rf bin dist coverage.out
