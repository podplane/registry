#!/usr/bin/env bash
# Podplane <https://podplane.dev>
# Copyright The Podplane Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
for command in curl lsof tmux; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "$command is required for the end-to-end test" >&2
		exit 1
	fi
done

: "${OCIMAGE_BIN:=ocimage}"
if ! "$OCIMAGE_BIN" version >/dev/null 2>&1; then
	echo "a working ocimage binary is required for the end-to-end test" >&2
	exit 1
fi

: "${OVERMIND_BIN:=overmind}"
if ! "$OVERMIND_BIN" --version >/dev/null 2>&1; then
	echo "a working Overmind binary is required for the end-to-end test" >&2
	exit 1
fi

if [[ -z ${WEED_BIN:-} ]]; then
	weed=$(command -v weed || true)
	if [[ -n "$weed" ]] && "$weed" version >/dev/null 2>&1; then
		WEED_BIN=$weed
	elif command -v mise >/dev/null 2>&1; then
		weed=$(mise where github:seaweedfs/seaweedfs@4.44 2>/dev/null || true)
		if [[ -x "$weed/weed" ]]; then
			WEED_BIN=$weed/weed
		fi
	fi
fi
export WEED_BIN=${WEED_BIN:-}
if [[ -z "$WEED_BIN" ]] || ! "$WEED_BIN" version >/dev/null 2>&1; then
	echo "SeaweedFS 4.44 is required; set WEED_BIN to its weed binary" >&2
	exit 1
fi

mkdir -p "$root/tmp"
export REGISTRY_E2E_ROOT
REGISTRY_E2E_ROOT=$(mktemp -d "$root/tmp/e2e.XXXXXX")
socket="$REGISTRY_E2E_ROOT/overmind.sock"
failed=1

# cleanup stops the development stack and removes its isolated storage.
cleanup() {
	if [[ -S "$socket" ]]; then
		if [[ "$failed" == 1 ]]; then
			"$OVERMIND_BIN" echo -s "$socket" &
			local log_pid=$!
			sleep 1
			kill "$log_pid" 2>/dev/null || true
			wait "$log_pid" 2>/dev/null || true
		fi
		"$OVERMIND_BIN" quit -s "$socket" >/dev/null 2>&1 || true
	fi
	rm -rf "$REGISTRY_E2E_ROOT"
}
trap cleanup EXIT INT TERM

# allocate_ports selects five consecutive unused localhost ports.
allocate_ports() {
	local candidate offset available
	for _ in {1..100}; do
		candidate=$((20000 + RANDOM % 30000))
		available=true
		for offset in {0..4}; do
			if lsof -nP -iTCP:"$((candidate + offset))" -sTCP:LISTEN >/dev/null 2>&1; then
				available=false
				break
			fi
		done
		if [[ "$available" == true ]]; then
			REGISTRY_E2E_MASTER_PORT=$candidate
			REGISTRY_E2E_VOLUME_PORT=$((candidate + 1))
			REGISTRY_E2E_FILER_PORT=$((candidate + 2))
			REGISTRY_E2E_S3_PORT=$((candidate + 3))
			REGISTRY_E2E_REGISTRY_PORT=$((candidate + 4))
			return
		fi
	done
	echo "could not find five unused localhost ports" >&2
	exit 1
}

export REGISTRY_E2E_MASTER_PORT REGISTRY_E2E_VOLUME_PORT REGISTRY_E2E_FILER_PORT REGISTRY_E2E_S3_PORT REGISTRY_E2E_REGISTRY_PORT
allocate_ports
export AWS_ACCESS_KEY_ID=registry-e2e AWS_SECRET_ACCESS_KEY=registry-e2e AWS_REGION=us-east-1 AWS_EC2_METADATA_DISABLED=true

mkdir -p "$REGISTRY_E2E_ROOT/bin" "$REGISTRY_E2E_ROOT/weed" "$REGISTRY_E2E_ROOT/context"
cp "$root/bin/registry" "$REGISTRY_E2E_ROOT/bin/registry"
printf '%s\n' 'hello from the read-only registry' >"$REGISTRY_E2E_ROOT/context/hello.txt"
cat >"$REGISTRY_E2E_ROOT/context/Containerfile" <<'EOF'
# Podplane <https://podplane.dev>
# Copyright The Podplane Authors
# SPDX-License-Identifier: Apache-2.0
FROM scratch
COPY hello.txt /hello.txt
EOF

export OCIMAGE_STORE="$REGISTRY_E2E_ROOT/oci"
image="127.0.0.1:$REGISTRY_E2E_REGISTRY_PORT/e2e/hello:v1"
"$OCIMAGE_BIN" build --tag "$image" "$REGISTRY_E2E_ROOT/context"
export REGISTRY_E2E_LAYOUT="$OCIMAGE_STORE/127.0.0.1:$REGISTRY_E2E_REGISTRY_PORT/e2e/hello"
test -s "$REGISTRY_E2E_LAYOUT/index.json"

"$OVERMIND_BIN" start -D -N -f "$root/Procfile" -d "$root" -s "$socket"
for _ in {1..240}; do
	if curl --silent --output /dev/null "http://127.0.0.1:$REGISTRY_E2E_S3_PORT/" 2>/dev/null && \
		curl --silent --fail "http://127.0.0.1:$REGISTRY_E2E_REGISTRY_PORT/healthz" >/dev/null 2>&1; then
		break
	fi
	sleep 0.25
done
curl --silent --output /dev/null "http://127.0.0.1:$REGISTRY_E2E_S3_PORT/"
curl --silent --fail "http://127.0.0.1:$REGISTRY_E2E_REGISTRY_PORT/healthz" >/dev/null

export REGISTRY_E2E=1
go test -count=1 ./internal/e2e

failed=0
echo "registry end-to-end test passed"
