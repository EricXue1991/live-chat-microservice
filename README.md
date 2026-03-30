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
