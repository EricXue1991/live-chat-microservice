#!/usr/bin/env bash
# Experiment 6 — Rate Limiting (system stability under abusive load).
# Runs two ChatUser passes: rate limiting ON (20 RPS/user) vs OFF.
# Uses high user count to ensure rate limits are actually triggered.
#
# Prerequisites:
#   pip install locust websocket-client
#
# Usage:
#   ./scripts/run_experiment6_local.sh
#   USERS=100 DURATION=90s ./scripts/run_experiment6_local.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${HOST:-http://localhost:8080}"
USERS="${USERS:-100}"
SPAWN="${SPAWN:-10}"
DURATION="${DURATION:-90s}"
STAMP=$(date +%Y%m%d_%H%M%S)
OUT_LIMITED="${ROOT}/scripts/results/exp6_${STAMP}_limited"
OUT_UNLIMITED="${ROOT}/scripts/results/exp6_${STAMP}_unlimited"

mkdir -p "$OUT_LIMITED" "$OUT_UNLIMITED"

if ! command -v locust >/dev/null 2>&1; then
  echo "Locust not found. Install: pip install locust websocket-client"
  exit 1
fi

echo "=== Experiment 6: Rate Limiting ==="
echo "Host: $HOST  users: $USERS  spawn: $SPAWN  duration: $DURATION"
echo ""

# --- Pass 1: Rate limiting ON (RATE_LIMIT_RPS=20 default) ---
echo "--- Pass 1/2: Rate Limiting ON (20 RPS/user) ---"
docker compose up -d --build
sleep 8
echo "CSV prefix: $OUT_LIMITED/locust"
locust -f "$ROOT/scripts/locustfile.py" \
  --headless --host "$HOST" \
  -u "$USERS" -r "$SPAWN" -t "$DURATION" \
  --csv "$OUT_LIMITED/locust" \
  ChatUser

echo ""
echo "Rate-limited pass done. Switching to unlimited..."
sleep 5

# --- Pass 2: Rate limiting OFF ---
echo "--- Pass 2/2: Rate Limiting OFF (RATE_LIMIT_RPS=0) ---"
docker compose -f docker-compose.yml -f docker-compose.exp6-overrides.yml up -d --build
sleep 8
echo "CSV prefix: $OUT_UNLIMITED/locust"
locust -f "$ROOT/scripts/locustfile.py" \
  --headless --host "$HOST" \
  -u "$USERS" -r "$SPAWN" -t "$DURATION" \
  --csv "$OUT_UNLIMITED/locust" \
  ChatUser

echo ""
echo "=== Both passes complete ==="
echo "Limited   CSV: $OUT_LIMITED/locust_stats.csv"
echo "Unlimited CSV: $OUT_UNLIMITED/locust_stats.csv"
echo ""
echo "Generate charts:"
echo "  python scripts/plot_experiment6.py \\"
echo "    --limited-csv   $OUT_LIMITED/locust_stats.csv \\"
echo "    --unlimited-csv $OUT_UNLIMITED/locust_stats.csv"
