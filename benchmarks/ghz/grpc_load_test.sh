#!/usr/bin/env bash
set -euo pipefail

# gRPC load testing using ghz
# Tool: https://ghz.ozon.ru/

TARGET="${GRPC_TARGET:-localhost:9090}"
PROTO_PATH="./api/proto/v1/gargi.proto"
CALL="gargi.v1.GargiService.GetExecution"

echo "=== Running ghz gRPC Load Test against ${TARGET} ==="

ghz \
  --insecure \
  --proto="${PROTO_PATH}" \
  --call="${CALL}" \
  --data='{"execution_id": "test-execution-id"}' \
  --total=10000 \
  --concurrency=50 \
  --cpus=4 \
  --duration=30s \
  "${TARGET}"
