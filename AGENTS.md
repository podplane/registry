# Registry Agent Guide

## Layout

- `pkg/registry`: public OCI Distribution HTTP handler.
- `pkg/storage`: provider-neutral storage contracts and S3/GCS adapters.
- `internal/cmd`: standalone command and process lifecycle.
- `internal/e2e`: local SeaweedFS publication and registry pull verification.
- `internal/buildvars`: release metadata.
- `cmd/registry`: executable entrypoint.
- `tools`: isolated Go module for pinned golangci-lint, ocimage, and Overmind development tools.

## Commands

- `make setup`: verify development tools and install Git hooks.
- `make generate`: regenerate root and tool module metadata.
- `make check-generated`: verify generated module metadata is current.
- `make fmt`: format Go files.
- `make precommit`: check formatting and vet.
- `make lint`: run golangci-lint.
- `make test`: run race-enabled tests.
- `make e2e`: build an image with ocimage and pull it through Registry backed by SeaweedFS.
- `make build`: build `bin/registry`.

## Constraints

- Keep registry HTTP behavior and provider storage strictly read-only and request-driven: no list, write, cache, database, startup probe, or background storage operation.
- `pkg/registry` must remain provider-neutral; configure provider behavior on injected S3 and GCS clients.
- Preserve the loopback/private-network trust boundary until authentication is explicitly designed.
- Every non-command Go package requires a `doc.go` package comment. Command packages keep their package comment in `main.go`. Every function and type requires a doc comment, including unexported and test declarations.
- Add the Podplane yearless Apache-2.0 header to project-owned comment-capable files.
- Generated files: root and `tools` `go.sum` files, maintained by `go mod tidy` in their respective modules.
