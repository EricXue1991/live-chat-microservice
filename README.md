# LiveChat — Distributed Live Chat + Reaction Aggregation

## Architecture

```
                        ┌──────────────┐
                        │  CloudFront  │ (optional CDN)
                        └──────┬───────┘
                               │
Client (React) ───────► ALB ──►│──► ECS (Go API x N replicas)
                               │        │
                    ┌──────────┼────────┼──────────┐
                    ▼          ▼        ▼          ▼
                PostgreSQL  DynamoDB  Redis      S3
                (users,     (messages,(cache,    (attachments)
                 rooms)      reactions)rate-limit)
                               │
                    ┌──────────┼──────────┐
                    ▼          ▼          ▼
                  SNS/SQS    Kafka     SQS
                (broadcast) (event    (reaction
                             stream)   queue)
                               │
                               ▼
                          Analytics
                        Consumer(s)
```

## Components

| Component     | Technology       | Purpose                                           |
| ------------- | ---------------- | ------------------------------------------------- |
| Auth & Users  | PostgreSQL + JWT | User accounts, room metadata, relational data     |
| Chat Messages | DynamoDB         | High-throughput message storage, room-partitioned |
| Reactions     | DynamoDB + SQS   | Atomic counters with async batch aggregation      |
| Cache         | Redis            | Hot data cache, rate limiting, online presence    |
| Event Stream  | Kafka            | Durable event log, analytics pipeline, replay     |
| Broadcast     | SNS → SQS        | Cross-replica WebSocket message fan-out           |
| Attachments   | S3               | File/image uploads                                |
| Load Test     | Locust           | Simulated users for all experiments               |

## Experiments

1. **Scale-Out** (1/2/4/8 replicas) — linear scaling validation
2. **Hot-Room vs Multi-Room** — DynamoDB partition throttling
3. **Sync vs Async Reactions** — SQS batch aggregation benefit
4. **WebSocket vs HTTP Polling** — push vs pull latency

### Experiment 1: Scale-Out (1/2/4/8 replicas)

Validates linear horizontal scaling by measuring throughput and latency as ECS Fargate replicas increase from 1 to 8 behind an Application Load Balancer.

**Setup**

- 150 simulated users (ChatUser), fixed load across all runs
- Replicas tested: 1, 2, 4, 8
- Each run: 120 seconds, 60-second cooldown between runs
- ALB distributes traffic across replicas via round-robin

**How to run (AWS)**

```bash
# Prerequisites: AWS CLI configured, Locust installed
pip install locust matplotlib

# Run full experiment (automatically scales ECS 1→2→4→8, restores to 2 after)
./scripts/run_experiment1_aws.sh

# Override defaults:
USERS=200 DURATION=180s ./scripts/run_experiment1_aws.sh

# Generate charts
python scripts/plot_experiment1.py --results-dir scripts/results/exp1_<timestamp>
```

Charts are saved to `report/figures/exp1/`.

**Results (150 users, AWS ECS Fargate)**

| Replicas | Throughput (req/s) | Avg Latency (ms) | p95 (ms) | p99 (ms) | Error Rate | Scaling Efficiency |
| -------- | ------------------ | ---------------- | -------- | -------- | ---------- | ------------------ |
| 1        | 38.2               | 1289             | 14000    | 22000    | 4.22%      | 100%               |
| 2        | 55.2               | 347              | 120      | 9500     | 3.8%       | 72.3%              |
| 4        | 56.5               | 323              | 110      | 8800     | 3.5%       | 37.0%              |
| 8        | 57.0               | 312              | 110      | 8500     | 3.2%       | 18.7%              |

**Key finding:** Scaling from 1→2 replicas yields a significant improvement (~44% throughput gain, ~73% latency reduction). However, beyond 2 replicas the gains plateau — throughput stabilizes around 55-57 req/s. This indicates the bottleneck shifts from compute to shared backend resources (PostgreSQL connections during registration, DynamoDB throughput, ALB connection handling). The high p99 latency across all configurations is driven by the initial registration/login burst hitting PostgreSQL.

---

### Experiment 2: Hot-Room vs Multi-Room

Measures the impact of concentrated traffic on a single DynamoDB partition key vs traffic distributed across 100 partition keys.

**Setup**

- **Hot Room:** 500 simulated users, ALL traffic to `room-hot` (single partition key)
- **Multi Room:** 500 simulated users, each assigned a random room from 100 rooms
- Both runs use identical task weights and 180-second duration
- Rate limiting and Redis cache disabled to isolate storage/application behavior

**How to run**

```bash
# 1. Start stack with experiment overrides
docker compose -f docker-compose.yml -f docker-compose.exp2-overrides.yml up -d --build

# 2. Run both passes (hot room then multi room, 30s cooldown between)
./scripts/run_experiment2_local.sh

# Override defaults:
USERS=500 DURATION=180s ./scripts/run_experiment2_local.sh

# 3. Generate charts
python scripts/plot_experiment2.py \
  --hot-csv   scripts/results/exp2_<timestamp>/hot/locust_stats.csv \
  --multi-csv scripts/results/exp2_<timestamp>/multi/locust_stats.csv \
  --hot-history   scripts/results/exp2_<timestamp>/hot/locust_stats_history.csv \
  --multi-history scripts/results/exp2_<timestamp>/multi/locust_stats_history.csv
```

Charts are saved to `report/figures/exp2/`.

**Results (500 users, LocalStack)**

| Metric                  | Hot Room    | Multi Room  |
| ----------------------- | ----------- | ----------- |
| Total Throughput        | 298.2 req/s | 278.5 req/s |
| Avg Latency             | 994 ms      | 1112 ms     |
| p99 Latency (reactions) | 1550 ms     | 2100 ms     |
| Error Rate              | 0%          | 0%          |

**Key finding:** Under LocalStack (no real DynamoDB partition limits), the hot room scenario outperformed multi room by 7% throughput and 35% lower p99 latency. This counterintuitive result is explained by application-layer locality — a single room means one hub map entry with less mutex contention, versus 100 entries with higher lock overhead. The time-series data confirms this: multi room throughput degrades from ~340 to ~250 req/s over 180 seconds, while hot room stays stable at ~300 req/s.

In production AWS DynamoDB, we expect this to reverse at higher loads when the single partition hits the 1,000 WCU/s throttling ceiling.

**How to run (AWS ECS)**

```bash
# Run against deployed ALB (auto-disables rate limit & cache, restores after)
./scripts/run_experiment2_aws.sh

# Override defaults:
USERS=200 DURATION=180s HOST=http://your-alb-dns ./scripts/run_experiment2_aws.sh

# Generate charts (saved to report/figures/exp2_aws/ to preserve local results)
python scripts/plot_experiment2.py \
  --hot-csv   scripts/results/exp2_<timestamp>/hot/locust_stats.csv \
  --multi-csv scripts/results/exp2_<timestamp>/multi/locust_stats.csv \
  --hot-history   scripts/results/exp2_<timestamp>/hot/locust_stats_history.csv \
  --multi-history scripts/results/exp2_<timestamp>/multi/locust_stats_history.csv \
  --out-dir report/figures/exp2_aws
```

Charts are saved to `report/figures/exp2_aws/`.

**Results (150 users, AWS ECS + real DynamoDB)**

| Metric           | Hot Room   | Multi Room |
| ---------------- | ---------- | ---------- |
| Total Throughput | 52.3 req/s | 54.8 req/s |
| Avg Latency      | 186 ms     | 142 ms     |
| p99 Latency      | 9200 ms    | 6800 ms    |
| Error Rate       | 4.1%       | 3.6%       |

**Key finding (AWS):** On real AWS DynamoDB, the results flip compared to LocalStack — multi-room outperforms hot-room by ~5% in throughput with 24% lower average latency and 26% lower p99. This validates the DynamoDB partition key design: concentrating all writes on a single partition key (`room-hot`) creates contention at the storage layer, while distributing across 100 partition keys allows DynamoDB to parallelize writes across multiple physical partitions. The error rate difference (4.1% vs 3.6%) further confirms that the hot partition experiences more throttling under load.

---

### Experiment 3 (sync vs async reactions)

1. Bring the stack up with rate limiting off and cache disabled so reactions are the bottleneck (aligned with AWS `run_experiment3_aws.sh`):

   `REACTION_MODE=async docker compose -f docker-compose.yml -f docker-compose.exp3-overrides.yml up -d --build`

2. Run a reaction-heavy Locust profile (writes mostly to one room):

   `./scripts/run_experiment3_local.sh`

   Override load with env vars, e.g. `USERS=120 DURATION=180s ./scripts/run_experiment3_local.sh`.

3. Repeat with sync mode (restart backend): `REACTION_MODE=sync` in the same `docker compose ...` command.

4. Compare Locust CSVs under `scripts/results/` and optional `GET /api/status` fields `reaction_queue_visible` / `reaction_queue_inflight` (async only). Report targets: POST `/api/reactions` throughput and p95/p99, DynamoDB pressure, queue backlog in async mode.

**How to run (AWS ECS)**

Same idea as Experiments 1–2: Locust hits the deployed ALB; the script temporarily sets `RATE_LIMIT_RPS=0`, `CACHE_ENABLED=false` (as in experiment 2 AWS), then runs **async** then **sync** via new ECS task definitions, then restores the service’s original task definition.

```bash
# Prerequisites: pip install locust matplotlib websocket-client, AWS CLI configured, stack deployed (Terraform + deploy.sh)
./scripts/run_experiment3_aws.sh

# Override defaults:
USERS=120 DURATION=180s HOST=http://your-alb-dns.elb.amazonaws.com ./scripts/run_experiment3_aws.sh
```

Charts (saved next to Experiment 2 AWS style under `report/figures/exp3_aws/`):

```bash
python scripts/plot_experiment3.py \
  --sync-csv   scripts/results/exp3_<timestamp>/sync/locust_stats.csv \
  --async-csv  scripts/results/exp3_<timestamp>/async/locust_stats.csv \
  --out-dir report/figures/exp3_aws
```

### Experiment 4 (WebSocket vs HTTP Polling)

Measures end-to-end message delivery latency for push (WebSocket) vs pull (HTTP polling) under concurrent load.

**Note:** A bug was identified and fixed before running this experiment. The original `chat/handler.go` routed all WebSocket delivery through SNS → SQS (WaitTimeSeconds=20), causing up to 20s latency even for same-replica clients. The fix adds a direct `hub.BroadcastToRoom()` call for instant local delivery (~180ms p50 under load), while SNS/SQS is retained for cross-replica fan-out. See [GitHub issue] for details.

1. Bring the stack up with rate limiting disabled so transport is the bottleneck:

   `docker compose -f docker-compose.yml -f docker-compose.exp4-overrides.yml up -d --build`

2. Run both Locust passes sequentially (PollingUser then WebSocketUser):

   `./scripts/run_experiment4_local.sh`

   Override load with env vars, e.g. `USERS=80 DURATION=120s ./scripts/run_experiment4_local.sh`.

3. Generate comparison charts from the two CSV outputs:

   ```
   python scripts/plot_experiment4.py \
     --polling-csv scripts/results/exp4_<timestamp>_polling/locust_stats.csv \
     --ws-csv      scripts/results/exp4_<timestamp>_ws/locust_stats.csv
   ```

   Charts are saved to `report/figures/exp4/`.

4. Compare results. Report targets: `POLL_LATENCY` vs `WS_LATENCY` e2e delivery at p50/p95/p99, average speedup ratio. Expected outcome: WebSocket ~3-5x lower p50 latency than polling under 50 concurrent users.
#### Pre-Fix Results (Initial Run)

| Metric | HTTP Polling | WebSocket | Speedup |
|--------|-------------|-----------|---------|
| p50 | 1000 ms | 17000 ms | 0.1x  |
| p95 | 3200 ms | 33000 ms | 0.1x |
| p99 | 4600 ms | 37000 ms | 0.1x |
| Average | 1261 ms | 17160 ms | 0.1x|
#### Post-Fix Results (Local)

After implementing direct hub broadcast in `chat/handler.go`, WebSocket correctly outperforms HTTP polling across all latency percentiles.

| Metric  | HTTP Polling | WebSocket | Speedup  |
|---------|-------------|-----------|----------|
| p50     | 970 ms      | 180 ms    | **5.4x** |
| p95     | 3300 ms     | 1000 ms   | **3.3x** |
| p99     | 5100 ms     | 2500 ms   | **2.0x** |
| Average | 1236 ms     | 328 ms    | **3.8x** |

#### AWS Results (50 users, ECS Fargate, us-west-2)

Run against the live AWS deployment with rate limiting disabled (`RATE_LIMIT_RPS=0`).

```bash
./scripts/run_experiment4_aws.sh
# Override: USERS=100 DURATION=120s ./scripts/run_experiment4_aws.sh
```

| Metric  | HTTP Polling | WebSocket | Speedup   |
|---------|-------------|-----------|-----------|
| p50     | 780 ms      | ~0 ms     | —         |
| p95     | 2500 ms     | 62 ms     | **40x**   |
| p99     | 3800 ms     | 120 ms    | **32x**   |
| Average | 942 ms      | 8 ms      | **116x**  |

**Key finding:** On AWS, WebSocket push latency is dramatically lower than local results due to real network conditions amplifying the polling overhead. The near-zero WS latency reflects direct hub broadcast delivering messages before the polling interval even begins. Charts: `report/figures/exp4_aws/`.

## Quick Start

```bash
docker-compose up --build
# Frontend: http://localhost:3000
# Backend:  http://localhost:8080/health
```

## Project Structure

```
livechat/
├── backend/
│   ├── cmd/server/main.go              # Entry point, wires all modules
│   └── internal/
│       ├── config/config.go            # Env config loader
│       ├── models/models.go            # Shared data models
│       ├── auth/handler.go             # Register/Login (PostgreSQL + JWT)
│       ├── middleware/jwt.go           # JWT verification middleware
│       ├── middleware/ratelimit.go     # Redis-based rate limiter
│       ├── chat/handler.go            # Chat messages (HTTP + DynamoDB)
│       ├── ws/hub.go                  # WebSocket hub + SNS fan-out
│       ├── ws/client.go               # WebSocket client connection
│       ├── reaction/handler.go        # Reaction submit + query
│       ├── reaction/aggregator.go     # SQS batch consumer
│       ├── media/handler.go           # S3 file upload
│       └── analytics/consumer.go      # Kafka event consumer
├── frontend/                           # React + Vite + Tailwind
├── infra/                              # Terraform
└── scripts/                            # Locust, deploy, init
```
