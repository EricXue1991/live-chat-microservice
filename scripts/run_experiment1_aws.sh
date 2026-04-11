#!/usr/bin/env bash
# ==============================================================================
# Experiment 1 — Scale-Out (1/2/4/8 replicas) on AWS ECS
# ==============================================================================
#
# This script:
#   1. Sets ECS desired count to N replicas
#   2. Waits for tasks to stabilize
#   3. Runs Locust load test
#   4. Repeats for each replica count
#   5. Generates comparison charts
#
# Prerequisites:
#   pip install locust matplotlib
#   AWS CLI configured with proper credentials
#
# Usage:
#   ./scripts/run_experiment1_aws.sh
#
# Override defaults:
#   USERS=200 DURATION=120s ./scripts/run_experiment1_aws.sh
#
# ==============================================================================

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Config ---
HOST="${HOST:-http://livechat-alb-551256164.us-east-1.elb.amazonaws.com}"
CLUSTER="livechat-cluster"
SERVICE="livechat-api"
REGION="${AWS_REGION:-us-east-1}"

USERS="${USERS:-150}"
SPAWN="${SPAWN:-15}"
DURATION="${DURATION:-120s}"
COOLDOWN="${COOLDOWN:-60}"
REPLICAS=(2 4 8)

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
OUT="${ROOT}/scripts/results/exp1_${TIMESTAMP}"
mkdir -p "${OUT}"

if ! command -v locust >/dev/null 2>&1; then
    echo "ERROR: Install Locust first:  pip install locust"
    exit 1
fi

echo "=============================================="
echo "  Experiment 1: Scale-Out Linear Scaling"
echo "=============================================="
echo "  Host:     ${HOST}"
echo "  Cluster:  ${CLUSTER}"
echo "  Users:    ${USERS}"
echo "  Spawn:    ${SPAWN}/s"
echo "  Duration: ${DURATION}"
echo "  Replicas: ${REPLICAS[*]}"
echo "  Output:   ${OUT}"
echo "=============================================="

# --- Health check ---
echo ""
echo "[0] Health check..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "${HOST}/health" || echo "000")
if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Backend not reachable at ${HOST}/health (HTTP ${HTTP_CODE})"
    exit 1
fi
echo "  Backend is healthy."

wait_for_stable() {
    local desired=$1
    local max_wait=300  # 5 minutes max
    local elapsed=0
    echo "  Waiting for ${desired} tasks to be running..."

    while [ $elapsed -lt $max_wait ]; do
        running=$(aws ecs describe-services \
            --cluster "${CLUSTER}" \
            --services "${SERVICE}" \
            --region "${REGION}" \
            --query 'services[0].runningCount' \
            --output text 2>/dev/null || echo "0")

        if [ "$running" = "$desired" ]; then
            echo "  ✓ ${running} tasks running and stable."
            # Extra wait for ALB health checks to pass
            echo "  Waiting 30s for ALB health checks..."
            sleep 30
            return 0
        fi

        echo "  ... ${running}/${desired} running (${elapsed}s elapsed)"
        sleep 10
        elapsed=$((elapsed + 10))
    done

    echo "  WARNING: Only ${running}/${desired} tasks after ${max_wait}s. Proceeding anyway."
    return 0
}

# --- Run each replica count ---
STEP=0
TOTAL=${#REPLICAS[@]}

for N in "${REPLICAS[@]}"; do
    STEP=$((STEP + 1))
    echo ""
    echo "=============================================="
    echo "  [${STEP}/${TOTAL}] Setting replicas = ${N}"
    echo "=============================================="

    # Scale ECS service
    aws ecs update-service \
        --cluster "${CLUSTER}" \
        --service "${SERVICE}" \
        --desired-count "${N}" \
        --region "${REGION}" \
        --no-cli-pager > /dev/null

    wait_for_stable "${N}"

    # Run Locust
    RUN_DIR="${OUT}/replicas_${N}"
    mkdir -p "${RUN_DIR}"

    echo ""
    echo "  Running Locust: ${USERS} users, ${DURATION}..."
    echo ""

    locust -f "${SCRIPT_DIR}/locustfile.py" \
        --headless \
        --host "${HOST}" \
        -u "${USERS}" \
        -r "${SPAWN}" \
        -t "${DURATION}" \
        --csv "${RUN_DIR}/locust" \
        --csv-full-history \
        ChatUser \
        2>&1 | tee "${RUN_DIR}/locust.log"

    echo ""
    echo "  ✓ Replicas=${N} complete. Results in ${RUN_DIR}"

    if [ "${STEP}" -lt "${TOTAL}" ]; then
        echo "  Cooling down ${COOLDOWN}s..."
        sleep "${COOLDOWN}"
    fi
done

# --- Restore to 2 replicas ---
echo ""
echo "Restoring service to 2 replicas..."
aws ecs update-service \
    --cluster "${CLUSTER}" \
    --service "${SERVICE}" \
    --desired-count 2 \
    --region "${REGION}" \
    --no-cli-pager > /dev/null

echo ""
echo "=============================================="
echo "  Experiment 1 Complete!"
echo "  Results: ${OUT}"
echo "=============================================="
echo ""
echo "  Generate charts:"
echo "    python scripts/plot_experiment1.py --results-dir ${OUT}"
echo "=============================================="