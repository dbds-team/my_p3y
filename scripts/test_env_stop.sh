#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME_DIR="$ROOT_DIR/.test_runtime"
BACKEND_PID="$RUNTIME_DIR/backend.pid"
P3Y_PID="$RUNTIME_DIR/p3y.pid"

stop_by_pid_file() {
  local name="$1"
  local pid_file="$2"

  if [[ ! -f "$pid_file" ]]; then
    echo "$name not running (no pid file)"
    return 0
  fi

  local pid
  pid="$(cat "$pid_file" 2>/dev/null || true)"
  if [[ -z "$pid" ]]; then
    rm -f "$pid_file"
    echo "$name not running (empty pid file)"
    return 0
  fi

  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    sleep 0.3
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
    echo "Stopped $name (pid=$pid)"
  else
    echo "$name already stopped (stale pid=$pid)"
  fi

  rm -f "$pid_file"
}

stop_by_pid_file "p3y" "$P3Y_PID"
stop_by_pid_file "backend" "$BACKEND_PID"

echo "Test env stopped"
