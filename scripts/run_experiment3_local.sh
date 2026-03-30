#!/usr/bin/env bash
# Experiment 3 — local Locust run (ReactionHeavyUser).
# Prerequisites: stack up with docker-compose + exp3 overrides; optional: poll /api/status during run for SQS depth.
#
#   REACTION_MODE=async docker compose -f docker-compose.yml -f docker-compose.exp3-overrides.yml up -d --build
#   ./scripts/run_experiment3_local.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOST="${HOST:-http://localhost:8080}"
USERS="${USERS:-80}"
SPAWN="${SPAWN:-10}"
DURATION="${DURATION:-120s}"
OUT="${ROOT}/scripts/results/exp3_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUT"

if ! command -v locust >/dev/null 2>&1; then
  echo "Install Locust: pip install locust websocket-client"
  exit 1
fi

echo "=== Experiment 3 local run ==="
echo "Host: $HOST  users: $USERS  spawn: $SPAWN  duration: $DURATION"
echo "CSV prefix: $OUT/locust"
echo "Tip: curl $HOST/api/status  (reaction_queue_* fields in async mode)"
echo

locust -f "$ROOT/scripts/locustfile.py" \
  --headless \
  --host "$HOST" \
  -u "$USERS" \
  -r "$SPAWN" \
  -t "$DURATION" \
  --csv "$OUT/locust" \
  ReactionHeavyUser

echo
echo "Done. Stats: $OUT/locust_stats.csv (and related CSVs)"
