# Podplane <https://podplane.dev>
# Copyright The Podplane Authors
# SPDX-License-Identifier: Apache-2.0

seaweed: "$WEED_BIN" server -dir="$REGISTRY_E2E_ROOT/weed" -ip=127.0.0.1 -ip.bind=127.0.0.1 -master.port="$REGISTRY_E2E_MASTER_PORT" -volume.port="$REGISTRY_E2E_VOLUME_PORT" -filer -filer.port="$REGISTRY_E2E_FILER_PORT" -s3 -s3.port="$REGISTRY_E2E_S3_PORT" -master.telemetry=false
registry: "$REGISTRY_E2E_ROOT/bin/registry" --listen="127.0.0.1:$REGISTRY_E2E_REGISTRY_PORT" --bucket=registry-e2e --region=us-east-1 --endpoint="http://127.0.0.1:$REGISTRY_E2E_S3_PORT" --path-style
