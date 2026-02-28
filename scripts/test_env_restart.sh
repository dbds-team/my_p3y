#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

"$ROOT_DIR/scripts/test_env_stop.sh" || true
"$ROOT_DIR/scripts/test_env_start.sh"
