#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME_DIR="$ROOT_DIR/.test_runtime"
BACKEND_PY="$RUNTIME_DIR/backend_server.py"
BACKEND_PID="$RUNTIME_DIR/backend.pid"
P3Y_PID="$RUNTIME_DIR/p3y.pid"
P3Y_BIN="$RUNTIME_DIR/p3y-test"
CERT_FILE="$RUNTIME_DIR/p3y-test.crt"
KEY_FILE="$RUNTIME_DIR/p3y-test.key"
BACKEND_LOG="$RUNTIME_DIR/backend.log"
P3Y_LOG="$RUNTIME_DIR/p3y.log"

BACKEND_ADDR="127.0.0.1"
BACKEND_PORT="18088"
PROXY_ADDR="0.0.0.0"
PROXY_PORT="60001"
CAPTURE_ADDR="0.0.0.0"
CAPTURE_PORT="60081"
METRICS_ADDR="127.0.0.1"
METRICS_PORT="62112"
CAPTURE_CFG="$ROOT_DIR/capture.yaml"

mkdir -p "$RUNTIME_DIR"

is_running() {
  local pid_file="$1"
  if [[ -f "$pid_file" ]]; then
    local pid
    pid="$(cat "$pid_file" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
  fi
  return 1
}

if is_running "$BACKEND_PID" || is_running "$P3Y_PID"; then
  echo "Test env already running. Stop it first: scripts/test_env_stop.sh"
  exit 1
fi

cat > "$BACKEND_PY" <<'PY'
from http.server import BaseHTTPRequestHandler, HTTPServer
import json

class Handler(BaseHTTPRequestHandler):
    def _send(self, payload):
        body = json.dumps(payload).encode('utf-8')
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self._send({"method": "GET", "path": self.path})

    def do_POST(self):
        n = int(self.headers.get('Content-Length', '0'))
        data = self.rfile.read(n).decode('utf-8') if n > 0 else ''
        self._send({"method": "POST", "path": self.path, "body": data})

    def log_message(self, fmt, *args):
        return

if __name__ == '__main__':
    HTTPServer(('127.0.0.1', 18088), Handler).serve_forever()
PY

if [[ ! -f "$CERT_FILE" || ! -f "$KEY_FILE" ]]; then
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$KEY_FILE" \
    -out "$CERT_FILE" \
    -days 1 \
    -subj '/CN=localhost' >/dev/null 2>&1
fi

(
  cd "$ROOT_DIR"
  go build -o "$P3Y_BIN" .
)

python3 "$BACKEND_PY" >"$BACKEND_LOG" 2>&1 &
echo $! > "$BACKEND_PID"

"$P3Y_BIN" \
  -ip "$PROXY_ADDR" \
  -metrics_ip "$METRICS_ADDR" \
  -capture_ip "$CAPTURE_ADDR" \
  -port "$PROXY_PORT" \
  -metrics_port "$METRICS_PORT" \
  -capture_port "$CAPTURE_PORT" \
  -backend "http://$BACKEND_ADDR:$BACKEND_PORT" \
  -tls \
  -crt "$CERT_FILE" \
  -key "$KEY_FILE" \
  -captureCfg "$CAPTURE_CFG" \
  >"$P3Y_LOG" 2>&1 &
echo $! > "$P3Y_PID"

sleep 1

if ! is_running "$BACKEND_PID"; then
  echo "Backend failed to start. See $BACKEND_LOG"
  exit 1
fi
if ! is_running "$P3Y_PID"; then
  echo "p3y failed to start. See $P3Y_LOG"
  exit 1
fi

echo "Started test env"
echo "- Backend: http://$BACKEND_ADDR:$BACKEND_PORT"
echo "- Proxy:   https://$PROXY_ADDR:$PROXY_PORT"
echo "- Capture: http://$CAPTURE_ADDR:$CAPTURE_PORT"
echo "- Metrics: http://$METRICS_ADDR:$METRICS_PORT/metrics"
echo "- Captures HTML: http://$CAPTURE_ADDR:$CAPTURE_PORT/captures"
echo "- Captures API:  http://$CAPTURE_ADDR:$CAPTURE_PORT/api/captures"
echo "- Backend PID: $(cat "$BACKEND_PID")"
echo "- p3y PID:     $(cat "$P3Y_PID")"
echo "- Logs:"
echo "  - $BACKEND_LOG"
echo "  - $P3Y_LOG"
echo "Quick check (local): curl -sk \"https://127.0.0.1:$PROXY_PORT/api/test?foo=bar\""

echo
echo "Processes:"
ps -fp "$(cat "$BACKEND_PID"),$(cat "$P3Y_PID")" || true

echo
echo "Listening ports:"
ss -lntp | grep -E ":($BACKEND_PORT|$PROXY_PORT|$CAPTURE_PORT|$METRICS_PORT)\\b" || true

echo
echo "Recent logs:"
echo "[backend]"
tail -n 5 "$BACKEND_LOG" || true
echo "[p3y]"
tail -n 10 "$P3Y_LOG" || true
