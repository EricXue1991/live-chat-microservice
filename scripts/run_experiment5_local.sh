#!/usr/bin/env bash
# Experiment 5 — Cache Hit vs Miss (Redis cache impact on read latency).
# Runs two ChatUser passes: cache ON (default) vs cache OFF.
#
# Prerequisites:
#   pip install locust websocket-client
#
# Usage:
#   ./scripts/run_experiment5_local.sh
#   USERS=60 DURATION=90s ./scripts/run_experiment5_local.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${HOST:-http://localhost:8080}"
USERS="${USERS:-60}"
SPAWN="${SPAWN:-5}"
DURATION="${DURATION:-90s}"
STAMP=$(date +%Y%m%d_%H%M%S)
OUT_CACHE_ON="${ROOT}/scripts/results/exp5_${STAMP}_cache_on"
OUT_CACHE_OFF="${ROOT}/scripts/results/exp5_${STAMP}_cache_off"

mkdir -p "$OUT_CACHE_ON" "$OUT_CACHE_OFF"

if ! command -v locust >/dev/null 2>&1; then
  echo "Locust not found. Install: pip install locust websocket-client"
  exit 1
fi

echo "=== Experiment 5: Cache Hit vs Miss ==="
echo "Host: $HOST  users: $USERS  spawn: $SPAWN  duration: $DURATION"
echo ""

# --- Pass 1: Cache ON (default stack) ---
echo "--- Pass 1/2: Cache ON ---"
echo "Starting stack with CACHE_ENABLED=true..."
docker compose up -d --build
sleep 8
echo "CSV prefix: $OUT_CACHE_ON/locust"
locust -f "$ROOT/scripts/locustfile.py" \
  --headless --host "$HOST" \
  -u "$USERS" -r "$SPAWN" -t "$DURATION" \
  --csv "$OUT_CACHE_ON/locust" \
  ChatUser

echo ""
echo "Cache ON pass done. Switching to cache OFF..."
sleep 5

# --- Pass 2: Cache OFF ---
echo "--- Pass 2/2: Cache OFF ---"
docker compose -f docker-compose.yml -f docker-compose.exp5-overrides.yml up -d --build
sleep 8
echo "CSV prefix: $OUT_CACHE_OFF/locust"
locust -f "$ROOT/scripts/locustfile.py" \
  --headless --host "$HOST" \
  -u "$USERS" -r "$SPAWN" -t "$DURATION" \
  --csv "$OUT_CACHE_OFF/locust" \
  ChatUser

echo ""
echo "=== Both passes complete ==="
echo "Cache ON  CSV: $OUT_CACHE_ON/locust_stats.csv"
echo "Cache OFF CSV: $OUT_CACHE_OFF/locust_stats.csv"
echo ""
echo "Generate charts:"
echo "  python scripts/plot_experiment5.py \\"
echo "    --cache-on-csv  $OUT_CACHE_ON/locust_stats.csv \\"
echo "    --cache-off-csv $OUT_CACHE_OFF/locust_stats.csv"
