#!/usr/bin/env bash
# Experiment 4 — WebSocket vs HTTP Polling latency comparison.
# Runs two Locust passes (PollingUser, then WebSocketUser) and saves CSVs.
#
# Prerequisites:
#   pip install locust websocket-client
#   docker compose -f docker-compose.yml -f docker-compose.exp4-overrides.yml up -d --build
#
# Usage:
#   ./scripts/run_experiment4_local.sh
#
# Override defaults:
#   USERS=50 DURATION=90s ./scripts/run_experiment4_local.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${HOST:-http://localhost:8080}"
USERS="${USERS:-50}"
SPAWN="${SPAWN:-5}"
DURATION="${DURATION:-90s}"
STAMP=$(date +%Y%m%d_%H%M%S)
OUT_POLL="${ROOT}/scripts/results/exp4_${STAMP}_polling"
OUT_WS="${ROOT}/scripts/results/exp4_${STAMP}_ws"

mkdir -p "$OUT_POLL" "$OUT_WS"

if ! command -v locust >/dev/null 2>&1; then
  echo "Locust not found. Install: pip install locust websocket-client"
  exit 1
fi

echo "=== Experiment 4: WebSocket vs HTTP Polling ==="
echo "Host: $HOST  users: $USERS  spawn: $SPAWN  duration: $DURATION"
echo ""

# --- Pass 1: HTTP Polling ---
echo "--- Pass 1/2: PollingUser (HTTP polling) ---"
echo "CSV prefix: $OUT_POLL/locust"
locust -f "$ROOT/scripts/locustfile.py" \
  --headless \
  --host "$HOST" \
  -u "$USERS" \
  -r "$SPAWN" \
  -t "$DURATION" \
  --csv "$OUT_POLL/locust" \
  PollingUser

echo ""
echo "Polling pass done. Waiting 5s before WebSocket pass..."
sleep 5

# --- Pass 2: WebSocket ---
echo "--- Pass 2/2: WebSocketUser (WebSocket push) ---"
echo "CSV prefix: $OUT_WS/locust"
locust -f "$ROOT/scripts/locustfile.py" \
  --headless \
  --host "$HOST" \
  -u "$USERS" \
  -r "$SPAWN" \
  -t "$DURATION" \
  --csv "$OUT_WS/locust" \
  WebSocketUser

echo ""
echo "=== Both passes complete ==="
echo "Polling CSV : $OUT_POLL/locust_stats.csv"
echo "WebSocket CSV: $OUT_WS/locust_stats.csv"
echo ""
echo "Generate charts:"
echo "  python scripts/plot_experiment4.py \\"
echo "    --polling-csv $OUT_POLL/locust_stats.csv \\"
echo "    --ws-csv      $OUT_WS/locust_stats.csv"
