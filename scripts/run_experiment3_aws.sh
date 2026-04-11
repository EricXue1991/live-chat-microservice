#!/usr/bin/env bash
# ==============================================================================
# Experiment 3 — Sync vs Async Reactions on AWS ECS
# ==============================================================================
#
# Aligns with docker-compose.exp3-overrides.yml: RATE_LIMIT_RPS=0, CACHE_ENABLED=false
# (same idea as experiment 2 AWS) so reactions are the bottleneck; toggles REACTION_MODE.
#
# This script:
#   1. Saves the current ECS task definition
#   2. Pass A: REACTION_MODE=async + RATE_LIMIT_RPS=0 + CACHE_ENABLED=false → Locust
#   3. Pass B: REACTION_MODE=sync  + RATE_LIMIT_RPS=0 + CACHE_ENABLED=false → Locust
#   4. Restores the original task definition
#
# Prerequisites:
#   pip install locust matplotlib
#   AWS CLI configured
#
# Usage:
#   ./scripts/run_experiment3_aws.sh
#
# Override:
#   USERS=120 DURATION=180s HOST=http://your-alb-dns.amazonaws.com ./scripts/run_experiment3_aws.sh
#
# Tip: poll queue depth during async run:  curl -s "$HOST/api/status" | jq .
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# --- Config (match run_experiment1_aws.sh / run_experiment2_aws.sh) ---
HOST="${HOST:-http://livechat-alb-551256164.us-east-1.elb.amazonaws.com}"
CLUSTER="livechat-cluster"
SERVICE="livechat-api"
REGION="${AWS_REGION:-us-east-1}"

USERS="${USERS:-80}"
SPAWN="${SPAWN:-10}"
DURATION="${DURATION:-120s}"
COOLDOWN="${COOLDOWN:-30}"

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
OUT="${ROOT}/scripts/results/exp3_${TIMESTAMP}"
mkdir -p "${OUT}/async" "${OUT}/sync"

if ! command -v locust >/dev/null 2>&1; then
  echo "ERROR: Install Locust first:  pip install locust websocket-client"
  exit 1
fi

echo "=============================================="
echo "  Experiment 3: Sync vs Async Reactions (AWS)"
echo "=============================================="
echo "  Host:     ${HOST}"
echo "  Users:    ${USERS}"
echo "  Spawn:    ${SPAWN}/s"
echo "  Duration: ${DURATION}"
echo "  Output:   ${OUT}"
echo "=============================================="

# --- Capture current task definition ARN ---
CURRENT_TD=$(aws ecs describe-services \
  --cluster "${CLUSTER}" \
  --services "${SERVICE}" \
  --region "${REGION}" \
  --query 'services[0].taskDefinition' \
  --output text)
echo "  Baseline task def: ${CURRENT_TD}"

apply_exp3_task_def() {
  local mode="$1"
  aws ecs describe-task-definition \
    --task-definition "${CURRENT_TD}" \
    --region "${REGION}" \
    --query 'taskDefinition.{containerDefinitions:containerDefinitions,family:family,networkMode:networkMode,requiresCompatibilities:requiresCompatibilities,cpu:cpu,memory:memory,executionRoleArn:executionRoleArn,taskRoleArn:taskRoleArn}' \
    --output json > /tmp/exp3-task-def.json

  python3 -c "
import json, sys
mode = sys.argv[1]
with open('/tmp/exp3-task-def.json') as f:
    td = json.load(f)
env = td['containerDefinitions'][0]['environment']
updates = {'RATE_LIMIT_RPS': '0', 'CACHE_ENABLED': 'false', 'REACTION_MODE': mode}
known = {e['name'] for e in env}
for k, v in updates.items():
    if k in known:
        for e in env:
            if e['name'] == k:
                e['value'] = v
                break
    else:
        env.append({'name': k, 'value': v})
with open('/tmp/exp3-task-def.json', 'w') as f:
    json.dump(td, f)
print('  Task def env: RATE_LIMIT_RPS=0, CACHE_ENABLED=false, REACTION_MODE=' + mode, file=sys.stderr)
" "${mode}"

  aws ecs register-task-definition \
    --cli-input-json file:///tmp/exp3-task-def.json \
    --region "${REGION}" \
    --query 'taskDefinition.taskDefinitionArn' \
    --output text
}

deploy_and_wait() {
  local td_arn="$1"
  aws ecs update-service \
    --cluster "${CLUSTER}" \
    --service "${SERVICE}" \
    --task-definition "${td_arn}" \
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
    if [ "${RUNNING}" = "${DESIRED}" ] && [ "${RUNNING}" != "0" ]; then
      echo "  ✓ ${RUNNING} tasks running with updated config."
      break
    fi
    echo "  ... ${RUNNING}/${DESIRED} running (${i}/30)"
    sleep 10
  done
  echo "  Waiting 30s for ALB health checks..."
  sleep 30
}

health_check() {
  echo ""
  echo "Health check..."
  HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "${HOST}/health" || echo "000")
  if [ "${HTTP_CODE}" != "200" ]; then
    echo "ERROR: Backend not reachable at ${HOST}/health (HTTP ${HTTP_CODE})"
    exit 1
  fi
  echo "  Backend is healthy."
}

# --- Pass A: async ---
echo ""
echo "[1/6] Deploying REACTION_MODE=async (RATE_LIMIT_RPS=0, CACHE_ENABLED=false)..."
NEW_ASYNC=$(apply_exp3_task_def async)
echo "  Registered: ${NEW_ASYNC}"
deploy_and_wait "${NEW_ASYNC}"
health_check

echo ""
echo "[2/6] Locust pass: ASYNC (ReactionHeavyUser)..."
# Locust exits non-zero when any request fails; with set -e + pipefail the pipeline would
# abort the script before [6/6] restore. Use `if !` so ECS is always restored.
if ! locust -f "${SCRIPT_DIR}/locustfile.py" \
  --headless \
  --host "${HOST}" \
  -u "${USERS}" \
  -r "${SPAWN}" \
  -t "${DURATION}" \
  --csv "${OUT}/async/locust" \
  --csv-full-history \
  ReactionHeavyUser \
  2>&1 | tee "${OUT}/async/locust.log"; then
  echo "WARNING: Locust async pass exited non-zero (see ${OUT}/async/locust.log)." >&2
fi

echo ""
echo "[3/6] Async pass complete. Cooling down ${COOLDOWN}s..."
sleep "${COOLDOWN}"

# --- Pass B: sync ---
echo ""
echo "[4/6] Deploying REACTION_MODE=sync (RATE_LIMIT_RPS=0, CACHE_ENABLED=false)..."
NEW_SYNC=$(apply_exp3_task_def sync)
echo "  Registered: ${NEW_SYNC}"
deploy_and_wait "${NEW_SYNC}"
health_check

echo ""
echo "[5/6] Locust pass: SYNC (ReactionHeavyUser)..."
if ! locust -f "${SCRIPT_DIR}/locustfile.py" \
  --headless \
  --host "${HOST}" \
  -u "${USERS}" \
  -r "${SPAWN}" \
  -t "${DURATION}" \
  --csv "${OUT}/sync/locust" \
  --csv-full-history \
  ReactionHeavyUser \
  2>&1 | tee "${OUT}/sync/locust.log"; then
  echo "WARNING: Locust sync pass exited non-zero (expected under overload, e.g. 503s). See ${OUT}/sync/locust.log." >&2
fi

# --- Restore baseline task definition ---
echo ""
echo "[6/6] Restoring original task definition..."
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
echo "  Experiment 3 Complete!"
echo "  Results: ${OUT}"
echo "=============================================="
echo ""
echo "  Generate charts (from repo root, after: pip install matplotlib):"
echo "    python scripts/plot_experiment3.py --results-dir ${OUT} --out-dir report/figures/exp3_aws"
echo "  Or:"
echo "    python scripts/plot_experiment3.py \\"
echo "      --async-csv ${OUT}/async/locust_stats.csv \\"
echo "      --sync-csv ${OUT}/sync/locust_stats.csv \\"
echo "      --out-dir report/figures/exp3_aws"
echo "=============================================="
