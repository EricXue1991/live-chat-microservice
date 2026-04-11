#!/usr/bin/env bash
# ==============================================================================
# Experiment 4 — WebSocket vs HTTP Polling latency on AWS ECS
# ==============================================================================
#
# This script:
#   1. Updates task definition (RATE_LIMIT_RPS=0) so transport is the bottleneck
#   2. Pass A: HTTP Polling (PollingUser)     — measures e2e POLL_LATENCY
#   3. Pass B: WebSocket push (WebSocketUser) — measures e2e WS_LATENCY
#   4. Restores original task definition
#
# Prerequisites:
#   pip install locust websocket-client
#   AWS CLI configured, ECS service running
#
# Usage:
#   ./scripts/run_experiment4_aws.sh
#
# Override defaults:
#   USERS=100 DURATION=120s ./scripts/run_experiment4_aws.sh
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Config ---
HOST="${HOST:-http://livechat-alb-1417974362.us-west-2.elb.amazonaws.com}"
CLUSTER="livechat-cluster"
SERVICE="livechat-api"
REGION="us-west-2"
TASK_FAMILY="livechat-api"

USERS="${USERS:-50}"
SPAWN="${SPAWN:-5}"
DURATION="${DURATION:-90s}"
COOLDOWN="${COOLDOWN:-30}"

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
OUT_POLL="${ROOT}/scripts/results/exp4_${TIMESTAMP}_polling"
OUT_WS="${ROOT}/scripts/results/exp4_${TIMESTAMP}_ws"

mkdir -p "$OUT_POLL" "$OUT_WS"

if ! command -v locust >/dev/null 2>&1; then
    echo "ERROR: Install Locust first:  pip install locust websocket-client"
    exit 1
fi

echo "=============================================="
echo "  Experiment 4: WebSocket vs HTTP Polling (AWS)"
echo "=============================================="
echo "  Host:     ${HOST}"
echo "  Users:    ${USERS}"
echo "  Spawn:    ${SPAWN}/s"
echo "  Duration: ${DURATION}"
echo "  Output:   ${ROOT}/scripts/results/exp4_${TIMESTAMP}_*"
echo "=============================================="

# --- Step 0: Update task def to disable rate limiting ---
echo ""
echo "[0/5] Updating task definition (RATE_LIMIT_RPS=0)..."

# Get current task definition ARN
CURRENT_TD=$(aws ecs describe-services \
    --cluster "${CLUSTER}" \
    --services "${SERVICE}" \
    --region "${REGION}" \
    --query 'services[0].taskDefinition' \
    --output text)
echo "  Current task def: ${CURRENT_TD}"

# Export task definition
aws ecs describe-task-definition \
    --task-definition "${CURRENT_TD}" \
    --region "${REGION}" \
    --query 'taskDefinition.{containerDefinitions:containerDefinitions,family:family,networkMode:networkMode,requiresCompatibilities:requiresCompatibilities,cpu:cpu,memory:memory,executionRoleArn:executionRoleArn,taskRoleArn:taskRoleArn}' \
    --output json > /tmp/exp4-task-def.json

# Set RATE_LIMIT_RPS=0 to disable rate limiting
python3 -c "
import json
with open('/tmp/exp4-task-def.json') as f:
    td = json.load(f)
env = td['containerDefinitions'][0]['environment']
for e in env:
    if e['name'] == 'RATE_LIMIT_RPS':
        e['value'] = '0'
with open('/tmp/exp4-task-def.json', 'w') as f:
    json.dump(td, f)
print('  Updated: RATE_LIMIT_RPS=0')
"

# Register new task definition revision
NEW_TD_ARN=$(aws ecs register-task-definition \
    --cli-input-json file:///tmp/exp4-task-def.json \
    --region "${REGION}" \
    --query 'taskDefinition.taskDefinitionArn' \
    --output text)
echo "  New task def: ${NEW_TD_ARN}"

# Update service
aws ecs update-service \
    --cluster "${CLUSTER}" \
    --service "${SERVICE}" \
    --task-definition "${NEW_TD_ARN}" \
    --force-new-deployment \
    --region "${REGION}" \
    --no-cli-pager > /dev/null

echo "  Waiting for new tasks to stabilize..."
sleep 10
for i in $(seq 1 30); do
    RUNNING=$(aws ecs describe-services \
        --cluster "${CLUSTER}" \
        --services "${SERVICE}" \
        --region "${REGION}" \
        --query 'services[0].runningCount' \
        --output text 2>/dev/null || echo "0")
    DESIRED=$(aws ecs describe-services \
        --cluster "${CLUSTER}" \
        --services "${SERVICE}" \
        --region "${REGION}" \
        --query 'services[0].desiredCount' \
        --output text 2>/dev/null || echo "0")
    if [ "$RUNNING" = "$DESIRED" ] && [ "$RUNNING" != "0" ]; then
        echo "  ✓ ${RUNNING} tasks running with updated config."
        break
    fi
    echo "  ... ${RUNNING}/${DESIRED} running (${i}/30)"
    sleep 10
done

echo "  Waiting 30s for ALB health checks..."
sleep 30

# --- Health check ---
echo ""
echo "[1/5] Health check..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "${HOST}/health" || echo "000")
if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Backend not reachable at ${HOST}/health (HTTP ${HTTP_CODE})"
    exit 1
fi
echo "  Backend is healthy."

# --- Pass A: HTTP Polling ---
echo ""
echo "[2/5] Running Pass A: HTTP Polling (PollingUser)..."
echo "  ${USERS} users, ${DURATION} duration"
echo ""

locust -f "${SCRIPT_DIR}/locustfile.py" \
    --headless \
    --host "${HOST}" \
    -u "${USERS}" \
    -r "${SPAWN}" \
    -t "${DURATION}" \
    --csv "${OUT_POLL}/locust" \
    --csv-full-history \
    PollingUser \
    2>&1 | tee "${OUT_POLL}/locust.log"

echo ""
echo "[3/5] Polling pass done. Cooling down ${COOLDOWN}s before WebSocket pass..."
sleep "${COOLDOWN}"

# --- Pass B: WebSocket ---
echo ""
echo "[4/5] Running Pass B: WebSocket push (WebSocketUser)..."
echo "  ${USERS} users, ${DURATION} duration"
echo ""

locust -f "${SCRIPT_DIR}/locustfile.py" \
    --headless \
    --host "${HOST}" \
    -u "${USERS}" \
    -r "${SPAWN}" \
    -t "${DURATION}" \
    --csv "${OUT_WS}/locust" \
    --csv-full-history \
    WebSocketUser \
    2>&1 | tee "${OUT_WS}/locust.log"

# --- Restore original task definition ---
echo ""
echo "[5/5] Restoring original task definition..."
aws ecs update-service \
    --cluster "${CLUSTER}" \
    --service "${SERVICE}" \
    --task-definition "${CURRENT_TD}" \
    --force-new-deployment \
    --region "${REGION}" \
    --no-cli-pager > /dev/null
echo "  Restored to: ${CURRENT_TD}"

echo ""
echo "=============================================="
echo "  Experiment 4 Complete!"
echo "=============================================="
echo "  Polling CSV:   ${OUT_POLL}/locust_stats.csv"
echo "  WebSocket CSV: ${OUT_WS}/locust_stats.csv"
echo ""
echo "  Generate charts:"
echo "    python scripts/plot_experiment4.py \\"
echo "      --polling-csv ${OUT_POLL}/locust_stats.csv \\"
echo "      --ws-csv      ${OUT_WS}/locust_stats.csv"
echo "=============================================="
