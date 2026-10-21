#!/usr/bin/env bash
# dev-up.sh — launch the full gargi-flow stack locally without Docker.
#
#   miniredis (scripts/redis_dev.go)  -> 127.0.0.1:6379
#   Temporal dev server               -> 127.0.0.1:7233 (UI on :8233)
#   API server (cmd/server)           -> :8080 (HTTP) / :9090 (gRPC)
#   Worker (cmd/worker)
#   Vite web UI (web/)                -> http://localhost:5173
#
# Logs are written to $GARGI_DEV_LOG_DIR (default: .dev-logs/). Press Ctrl+C to
# stop everything; all spawned processes are cleaned up on exit.
#
# Environment knobs:
#   GARGI_DEV_SKIP_TEMPORAL=1  don't start Temporal (use one you already run)
#   GARGI_DEV_SKIP_WEB=1       don't start the Vite dev server
#   GARGI_DEV_LOG_DIR=path     where per-service logs go

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LOG_DIR="${GARGI_DEV_LOG_DIR:-$ROOT_DIR/.dev-logs}"
WEB_PORT="${GARGI_DEV_WEB_PORT:-5173}"

PIDS=()
NAMES=()

info() { printf '\033[1;34m[dev-up]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[dev-up]\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[1;31m[dev-up]\033[0m %s\n' "$*" >&2; exit 1; }

CLEANED_UP=0
cleanup() {
  local rc=$?
  [[ $CLEANED_UP -eq 1 ]] && return
  CLEANED_UP=1
  trap - INT TERM EXIT
  if [[ ${#PIDS[@]} -gt 0 ]]; then
    info "Stopping ${#PIDS[@]} process(es)..."
    # Each service runs in its own process group (setsid), so signal the whole
    # group: `go run` and `npm run` spawn children that must die too.
    for pid in "${PIDS[@]}"; do
      kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
    done
    for _ in $(seq 1 50); do
      local alive=0
      for pid in "${PIDS[@]}"; do
        kill -0 "$pid" 2>/dev/null && alive=1
      done
      [[ $alive -eq 0 ]] && break
      sleep 0.1
    done
    for pid in "${PIDS[@]}"; do
      kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
    done
  fi
  info "All services stopped. Logs kept in $LOG_DIR"
  # Exit explicitly so an INT/TERM doesn't resume the monitor loop below.
  exit "$rc"
}
trap cleanup INT TERM EXIT

# start NAME CMD... — run CMD in the background in its own process group.
start() {
  local name="$1"
  shift
  local log="$LOG_DIR/$name.log"
  if command -v setsid >/dev/null 2>&1; then
    setsid "$@" >"$log" 2>&1 &
  else
    "$@" >"$log" 2>&1 &
  fi
  PIDS+=("$!")
  NAMES+=("$name")
  info "Started $name (pid $!) — logs: $log"
}

# wait_for_port NAME HOST PORT TIMEOUT_SECONDS
wait_for_port() {
  local name="$1" host="$2" port="$3" timeout="$4"
  for _ in $(seq 1 $((timeout * 2))); do
    if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
      info "$name is ready on $host:$port"
      return 0
    fi
    sleep 0.5
  done
  fail "$name did not become ready on $host:$port within ${timeout}s (see $LOG_DIR/)"
}

port_in_use() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

# --- Prerequisites ----------------------------------------------------------

missing=()
for bin in go node npm; do
  command -v "$bin" >/dev/null 2>&1 || missing+=("$bin")
done
[[ ${#missing[@]} -eq 0 ]] || fail "Missing required tools: ${missing[*]}"

# The official installer (temporal.download/cli.sh) puts the CLI here without
# necessarily adding it to PATH.
if ! command -v temporal >/dev/null 2>&1 && [[ -x "$HOME/.temporalio/bin/temporal" ]]; then
  export PATH="$HOME/.temporalio/bin:$PATH"
fi

START_TEMPORAL=1
if [[ "${GARGI_DEV_SKIP_TEMPORAL:-0}" == "1" ]]; then
  START_TEMPORAL=0
  info "GARGI_DEV_SKIP_TEMPORAL=1 — expecting Temporal on 127.0.0.1:7233"
elif ! command -v temporal >/dev/null 2>&1; then
  START_TEMPORAL=0
  if port_in_use 7233; then
    info "temporal CLI not found, but something is listening on :7233 — using it"
  else
    fail "$(cat <<'EOF'
The Temporal CLI ('temporal') was not found and nothing is listening on :7233.
Install it with one of:
    brew install temporal
    curl -sSf https://temporal.download/cli.sh | sh   # installs to ~/.temporalio/bin
Then re-run this script, or start Temporal yourself and set GARGI_DEV_SKIP_TEMPORAL=1.
EOF
)"
  fi
fi

if port_in_use 6379; then
  fail "Port 6379 is already in use (a Redis already running?). Stop it and retry."
fi

if [[ "${GARGI_DEV_SKIP_WEB:-0}" != "1" && ! -d web/node_modules ]]; then
  info "Installing web dependencies (npm ci)..."
  (cd web && npm ci)
fi

mkdir -p "$LOG_DIR"

# Server and worker are separate processes, so they must share state through
# Redis (the default `memory` driver would give each its own store). The Vite
# origin must also be allowed by the API's CORS middleware.
export GARGI_STORE_DRIVER="${GARGI_STORE_DRIVER:-redis}"
export GARGI_STORE_REDIS_ADDR="${GARGI_STORE_REDIS_ADDR:-127.0.0.1:6379}"
export GARGI_TEMPORAL_HOST_PORT="${GARGI_TEMPORAL_HOST_PORT:-127.0.0.1:7233}"
export GARGI_HTTP_CORS_ALLOWED_ORIGINS="${GARGI_HTTP_CORS_ALLOWED_ORIGINS:-http://localhost:$WEB_PORT,http://127.0.0.1:$WEB_PORT}"

# --- Launch -----------------------------------------------------------------

start redis go run ./scripts/redis_dev.go
wait_for_port "miniredis" 127.0.0.1 6379 60

if [[ $START_TEMPORAL -eq 1 ]]; then
  start temporal temporal server start-dev --ip 127.0.0.1 --ui-port 8233
fi
wait_for_port "Temporal" 127.0.0.1 7233 60

start server go run ./cmd/server/main.go
start worker go run ./cmd/worker/main.go
wait_for_port "API server" 127.0.0.1 8080 120

if [[ "${GARGI_DEV_SKIP_WEB:-0}" != "1" ]]; then
  start web npm --prefix web run dev -- --port "$WEB_PORT" --strictPort
  wait_for_port "Web UI" 127.0.0.1 "$WEB_PORT" 60
fi

cat <<EOF

  gargi-flow dev stack is up:
    Web UI       http://localhost:$WEB_PORT
    REST API     http://localhost:8080
    gRPC         localhost:9090
    Temporal UI  http://localhost:8233
    Logs         $LOG_DIR/

  Press Ctrl+C to stop everything.

EOF

# Exit (and clean up) as soon as any service dies.
while true; do
  for i in "${!PIDS[@]}"; do
    if ! kill -0 "${PIDS[$i]}" 2>/dev/null; then
      warn "${NAMES[$i]} exited unexpectedly — see $LOG_DIR/${NAMES[$i]}.log"
      exit 1
    fi
  done
  sleep 1
done
