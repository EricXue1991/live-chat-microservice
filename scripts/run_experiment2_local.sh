#!/usr/bin/env bash
# Experiment 2 — Hot Room vs Multi-Room traffic distribution.
# Runs two ChatUser passes: 90% traffic to one room (hot) vs 10% (distributed).
#
# Prerequisites:
#   docker compose up -d --build
#   pip install locust websocket-client
#
# Usage:
#   ./scripts/run_experiment2_local.sh
#   USERS=80 DURATION=120s ./scripts/run_experiment2_local.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${HOST:-http://localhost:8080}"
USERS="${USERS:-60}"
SPAWN="${SPAWN:-5}"
DURATION="${DURATION:-90s}"
STAMP=$(date +%Y%m%d_%H%M%S)
OUT_HOT="${ROOT}/scripts/results/exp2_${STAMP}_hot"
OUT_DIST="${ROOT}/scripts/results/exp2_${STAMP}_distributed"

mkdir -p "$OUT_HOT" "$OUT_DIST"

if ! command -v locust >/dev/null 2>&1; then
  echo "Locust not found. Install: pip install locust websocket-client"
  exit 1
fi

echo "=== Experiment 2: Hot Room vs Multi-Room ==="
echo "Host: $HOST  users: $USERS  spawn: $SPAWN  duration: $DURATION"
echo ""

# --- Pass 1: Hot Room (90% traffic to one room) ---
echo "--- Pass 1/2: Hot Room (HOT_ROOM_RATIO=0.9) ---"
HOT_ROOM_RATIO=0.9 locust -f "$ROOT/scripts/locustfile.py" \
  --headless --host "$HOST" \
  -u "$USERS" -r "$SPAWN" -t "$DURATION" \
  --csv "$OUT_HOT/locust" \
  ChatUser

echo ""
echo "Hot room pass done. Waiting 5s..."
sleep 5

# --- Pass 2: Distributed (10% to hot room, rest spread) ---
echo "--- Pass 2/2: Distributed (HOT_ROOM_RATIO=0.1) ---"
HOT_ROOM_RATIO=0.1 locust -f "$ROOT/scripts/locustfile.py" \
  --headless --host "$HOST" \
  -u "$USERS" -r "$SPAWN" -t "$DURATION" \
  --csv "$OUT_DIST/locust" \
  ChatUser

echo ""
echo "=== Both passes complete ==="
echo "Hot CSV        : $OUT_HOT/locust_stats.csv"
echo "Distributed CSV: $OUT_DIST/locust_stats.csv"
echo ""
echo "Generate charts:"
echo "  python scripts/plot_experiment2.py \\"
echo "    --hot-csv  $OUT_HOT/locust_stats.csv \\"
echo "    --dist-csv $OUT_DIST/locust_stats.csv"
