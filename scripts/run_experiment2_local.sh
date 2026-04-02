#!/usr/bin/env bash
# ==============================================================================
# Experiment 2 — Hot-Room vs Multi-Room
# ==============================================================================
#
# Prerequisites:
#   1. Stack is running:
#      docker compose -f docker-compose.yml -f docker-compose.exp2-overrides.yml up -d --build
#
#   2. Locust installed:
#      pip install locust
#
# Usage:
#   ./run_experiment2_local.sh
#
# Override defaults with env vars:
#   USERS=200 DURATION=180s HOST=http://your-alb-dns ./run_experiment2_local.sh
#
# Output:
#   scripts/results/exp2_<timestamp>/
#     ├── hot/locust_stats.csv          (hot room pass)
#     ├── hot/locust_stats_history.csv
#     ├── multi/locust_stats.csv        (multi room pass)
#     └── multi/locust_stats_history.csv
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

HOST="${HOST:-http://localhost:8080}"
USERS="${USERS:-150}"
SPAWN="${SPAWN:-15}"
DURATION="${DURATION:-120s}"
COOLDOWN="${COOLDOWN:-30}"

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
OUT="${ROOT}/scripts/results/exp2_${TIMESTAMP}"
mkdir -p "${OUT}/hot" "${OUT}/multi"

if ! command -v locust >/dev/null 2>&1; then
    echo "ERROR: Install Locust first:  pip install locust"
    exit 1
fi

echo "=============================================="
echo "  Experiment 2: Hot-Room vs Multi-Room"
echo "=============================================="
echo "  Host:     ${HOST}"
echo "  Users:    ${USERS}"
echo "  Spawn:    ${SPAWN}/s"
echo "  Duration: ${DURATION}"
echo "  Output:   ${OUT}"
echo "=============================================="

# --- Health check ---
echo ""
echo "[0/4] Health check..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "${HOST}/health" || echo "000")
if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Backend not reachable at ${HOST}/health (HTTP ${HTTP_CODE})"
    echo "Start the stack first:"
    echo "  docker compose -f docker-compose.yml -f docker-compose.exp2-overrides.yml up -d --build"
    exit 1
fi
echo "  Backend is healthy."

# --- Pass A: Hot Room ---
echo ""
echo "[1/4] Running Pass A: HOT ROOM (all traffic → room-hot)..."
echo "  ${USERS} users, ${DURATION} duration"
echo ""

locust -f "${SCRIPT_DIR}/locustfile_exp2.py" \
    --headless \
    --host "${HOST}" \
    -u "${USERS}" \
    -r "${SPAWN}" \
    -t "${DURATION}" \
    --csv "${OUT}/hot/locust" \
    --csv-full-history \
    HotRoomUser \
    2>&1 | tee "${OUT}/hot/locust.log"

echo ""
echo "[2/4] Hot room pass complete. Cooling down ${COOLDOWN}s..."
sleep "${COOLDOWN}"

# --- Pass B: Multi Room ---
echo ""
echo "[3/4] Running Pass B: MULTI ROOM (traffic → 100 rooms)..."
echo "  ${USERS} users, ${DURATION} duration"
echo ""

locust -f "${SCRIPT_DIR}/locustfile_exp2.py" \
    --headless \
    --host "${HOST}" \
    -u "${USERS}" \
    -r "${SPAWN}" \
    -t "${DURATION}" \
    --csv "${OUT}/multi/locust" \
    --csv-full-history \
    MultiRoomUser \
    2>&1 | tee "${OUT}/multi/locust.log"

echo ""
echo "[4/4] Both passes complete!"
echo ""
echo "=============================================="
echo "  Results saved to: ${OUT}"
echo "=============================================="
echo ""
echo "  Generate charts:"
echo "    python scripts/plot_experiment2.py \\"
echo "      --hot-csv ${OUT}/hot/locust_stats.csv \\"
echo "      --multi-csv ${OUT}/multi/locust_stats.csv"
echo ""
echo "  Compare summary:"
echo "    Hot room:   ${OUT}/hot/locust_stats.csv"
echo "    Multi room: ${OUT}/multi/locust_stats.csv"
echo "=============================================="
