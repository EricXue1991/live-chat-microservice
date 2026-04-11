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

---

### Experiment 3 (sync vs async reactions)

1. Bring the stack up with relaxed rate limits so reactions are the bottleneck:

   `REACTION_MODE=async docker compose -f docker-compose.yml -f docker-compose.exp3-overrides.yml up -d --build`

2. Run a reaction-heavy Locust profile (writes mostly to one room):

   `./scripts/run_experiment3_local.sh`

   Override load with env vars, e.g. `USERS=120 DURATION=180s ./scripts/run_experiment3_local.sh`.

3. Repeat with sync mode (restart backend): `REACTION_MODE=sync` in the same `docker compose ...` command.

4. Compare Locust CSVs under `scripts/results/` and optional `GET /api/status` fields `reaction_queue_visible` / `reaction_queue_inflight` (async only). Report targets: POST `/api/reactions` throughput and p95/p99, DynamoDB pressure, queue backlog in async mode.

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

**Key finding:** On AWS, WebSocket push latency is dramatically lower than local results due to real network conditions amplifying the polling overhead. The near-zero WS latency reflects direct hub broadcast delivering messages before the polling interval even begins. Charts: `report/figures/exp4/exp4_*_aws.png`.

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
