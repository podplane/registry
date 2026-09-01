# Podplane Registry

A read-only container registry that implements the pull subset of the [OCI Distribution Specification](https://specs.opencontainers.org/distribution-spec/) and serves an OCI image layout directly from object storage.

It can be imported as a Go package or run as a standalone process using Google Cloud Storage, AWS S3, or an S3-compatible object storage backend such as SeaweedFS.

The registry performs no startup probes, background calls, listing, caching, databases, or writes. Every storage call is caused by a valid GET or HEAD request.

A full blob GET performs one object read. A manifest GET performs one index read and one manifest read. HEAD and ranged requests additionally read object metadata when needed for response headers and range validation.

Registry, by design, does not support authentication or TLS. Keep the listener on loopback or behind an authenticated private-network proxy. Do not expose it directly to an untrusted network.

It is designed for use as a local read-only registry process on VMs in [Podplane](https://podplane.dev) and [Podmin](https://podmin.dev) clusters.

## Include it as a Go package

```go
handler, err := registry.New(store)
```

`pkg/registry` owns `/v2/`.

The registry handler and both provider implementations expose only the `storage.Reader` interface, which can report an object's size and open a full object or one inclusive range.

Object keys follow the OCI layout: `<repo>/index.json` and `<repo>/blobs/<algorithm>/<hex>`.

`pkg/storage/s3.New(client, bucket)` accepts an already configured AWS SDK v2 client. `pkg/storage/gcs.New(client, bucket)` accepts an already configured Google Cloud Storage client. Callers retain control of credentials, endpoints, retries, and provider-specific behavior.

## Run it as a standalone process

```console
registry --provider s3 --bucket images --region us-east-1 --listen 127.0.0.1:5000
registry --provider gcs --bucket images --listen 127.0.0.1:5000
```

Flags have environment equivalents: `REGISTRY_LISTEN`, `REGISTRY_PROVIDER`, `REGISTRY_BUCKET`, `AWS_REGION`, `REGISTRY_S3_ENDPOINT`, `REGISTRY_S3_PATH_STYLE`, and `AWS_PROFILE`. GCS uses Application Default Credentials. `/healthz` is a local liveness endpoint and never accesses storage.

## Development and releases

Run `make setup`, then `make check-generated precommit lint test build`.

`make e2e` uses the pinned Overmind and ocimage tools to run the registry and a loopback-only single-node SeaweedFS instance, build a scratch image, publish its OCI layout to S3, and verify a manifest, configuration, layer, HEAD request, byte range, and extracted file through the registry.

Semantic-version tags publish GoReleaser archives, SHA-512 checksums, SBOMs, a keyless Cosign checksum bundle, and GitHub artifact attestations through the release workflow.

## Learn More

Learn more about Podplane at the official project website: [podplane.dev](https://podplane.dev).

## License

Podplane is licensed under the Apache License, Version 2.0.
Copyright The Podplane Authors.

See the [LICENSE](./LICENSE) file for details.
