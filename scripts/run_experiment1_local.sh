#!/usr/bin/env bash
# Experiment 1 — Scale-Out: measure throughput vs backend replica count.
# Runs three passes: 1, 2, and 3 replicas behind an nginx load balancer.
#
# Prerequisites:
#   docker compose -f docker-compose.exp1.yml build
#   pip install locust websocket-client
#
# Usage:
#   ./scripts/run_experiment1_local.sh
#   USERS=80 DURATION=120s ./scripts/run_experiment1_local.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${HOST:-http://localhost:8081}"
USERS="${USERS:-60}"
SPAWN="${SPAWN:-5}"
DURATION="${DURATION:-90s}"
STAMP=$(date +%Y%m%d_%H%M%S)

if ! command -v locust >/dev/null 2>&1; then
  echo "Locust not found. Install: pip install locust websocket-client"
  exit 1
fi

echo "=== Experiment 1: Scale-Out ==="
echo "Host: $HOST  users: $USERS  spawn: $SPAWN  duration: $DURATION"
echo ""

for REPLICAS in 1 2 3; do
  OUT="${ROOT}/scripts/results/exp1_${STAMP}_r${REPLICAS}"
  mkdir -p "$OUT"

  echo "--- Pass: $REPLICAS replica(s) ---"
  docker compose -f docker-compose.exp1.yml up -d --scale backend="$REPLICAS" --build
  echo "Waiting 15s for replicas to be ready..."
  sleep 15

  locust -f "$ROOT/scripts/locustfile.py" \
    --headless --host "$HOST" \
    -u "$USERS" -r "$SPAWN" -t "$DURATION" \
    --csv "$OUT/locust" \
    ChatUser

  echo "Pass $REPLICAS done. Waiting 5s..."
  sleep 5
  echo ""
done

echo "=== All passes complete ==="
STAMP_GLOB="exp1_${STAMP}"
echo "Results in: scripts/results/${STAMP_GLOB}_r{1,2,3}/"
echo ""
echo "Generate charts:"
echo "  python scripts/plot_experiment1.py \\"
echo "    --r1-csv scripts/results/${STAMP_GLOB}_r1/locust_stats.csv \\"
echo "    --r2-csv scripts/results/${STAMP_GLOB}_r2/locust_stats.csv \\"
echo "    --r3-csv scripts/results/${STAMP_GLOB}_r3/locust_stats.csv"
