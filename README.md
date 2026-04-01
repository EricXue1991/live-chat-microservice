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

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Auth & Users | PostgreSQL + JWT | User accounts, room metadata, relational data |
| Chat Messages | DynamoDB | High-throughput message storage, room-partitioned |
| Reactions | DynamoDB + SQS | Atomic counters with async batch aggregation |
| Cache | Redis | Hot data cache, rate limiting, online presence |
| Event Stream | Kafka | Durable event log, analytics pipeline, replay |
| Broadcast | SNS → SQS | Cross-replica WebSocket message fan-out |
| Attachments | S3 | File/image uploads |
| Load Test | Locust | Simulated users for all experiments |

## Experiments

1. **Scale-Out** (1/2/4/8 replicas) — linear scaling validation
2. **Hot-Room vs Multi-Room** — DynamoDB partition throttling
3. **Sync vs Async Reactions** — SQS batch aggregation benefit
4. **WebSocket vs HTTP Polling** — push vs pull latency
5. **Cache Hit vs Miss** — Redis cache impact on read latency
6. **Rate Limiting** — system stability under abuse

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
