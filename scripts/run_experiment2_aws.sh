#!/usr/bin/env bash
# ==============================================================================
# Experiment 2 — Hot-Room vs Multi-Room on AWS ECS
# ==============================================================================
#
# This script:
#   1. Disables rate limiting & cache via new task definition (so DynamoDB is the bottleneck)
#   2. Pass A: all traffic → single room (hot partition key)
#   3. Pass B: traffic spread across 100 rooms (distributed partition keys)
#   4. Restores original task definition
#
# Prerequisites:
#   pip install locust matplotlib
#   AWS CLI configured
#
# Usage:
#   ./scripts/run_experiment2_aws.sh
#
# Override:
#   USERS=200 DURATION=180s ./scripts/run_experiment2_aws.sh
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Config ---
HOST="${HOST:-http://livechat-alb-551256164.us-east-1.elb.amazonaws.com}"
CLUSTER="livechat-cluster"
SERVICE="livechat-api"
REGION="us-east-1"
TASK_FAMILY="livechat-api"

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
echo "  Experiment 2: Hot-Room vs Multi-Room (AWS)"
echo "=============================================="
echo "  Host:     ${HOST}"
echo "  Users:    ${USERS}"
echo "  Spawn:    ${SPAWN}/s"
echo "  Duration: ${DURATION}"
echo "  Output:   ${OUT}"
echo "=============================================="

# --- Step 0: Update task def to disable rate limit & cache ---
echo ""
echo "[0/5] Updating task definition (RATE_LIMIT_RPS=0, CACHE_ENABLED=false)..."

# Get current task definition
CURRENT_TD=$(aws ecs describe-services \
    --cluster "${CLUSTER}" \
    --services "${SERVICE}" \
    --region "${REGION}" \
    --query 'services[0].taskDefinition' \
    --output text)
echo "  Current task def: ${CURRENT_TD}"

# Export and modify
aws ecs describe-task-definition \
    --task-definition "${CURRENT_TD}" \
    --region "${REGION}" \
    --query 'taskDefinition.{containerDefinitions:containerDefinitions,family:family,networkMode:networkMode,requiresCompatibilities:requiresCompatibilities,cpu:cpu,memory:memory,executionRoleArn:executionRoleArn,taskRoleArn:taskRoleArn}' \
    --output json > /tmp/exp2-task-def.json

# Use python to reliably modify env vars in JSON
python3 -c "
import json
with open('/tmp/exp2-task-def.json') as f:
    td = json.load(f)
env = td['containerDefinitions'][0]['environment']
updates = {'RATE_LIMIT_RPS': '0', 'CACHE_ENABLED': 'false'}
for e in env:
    if e['name'] in updates:
        e['value'] = updates[e['name']]
with open('/tmp/exp2-task-def.json', 'w') as f:
    json.dump(td, f)
print('  Updated: RATE_LIMIT_RPS=0, CACHE_ENABLED=false')
"

# Register new revision
NEW_TD_ARN=$(aws ecs register-task-definition \
    --cli-input-json file:///tmp/exp2-task-def.json \
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
# Wait for running count to match desired
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

# Extra wait for ALB health checks
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

# --- Pass A: Hot Room ---
echo ""
echo "[2/5] Running Pass A: HOT ROOM (all traffic → room-hot)..."
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
echo "[3/5] Hot room pass complete. Cooling down ${COOLDOWN}s..."
sleep "${COOLDOWN}"

# --- Pass B: Multi Room ---
echo ""
echo "[4/5] Running Pass B: MULTI ROOM (traffic → 100 rooms)..."
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
echo "  Experiment 2 Complete!"
echo "  Results: ${OUT}"
echo "=============================================="
echo ""
echo "  Generate charts:"
echo "    python scripts/plot_experiment2.py \\"
echo "      --hot-csv ${OUT}/hot/locust_stats.csv \\"
echo "      --multi-csv ${OUT}/multi/locust_stats.csv \\"
echo "      --hot-history ${OUT}/hot/locust_stats_history.csv \\"
echo "      --multi-history ${OUT}/multi/locust_stats_history.csv"
echo "=============================================="