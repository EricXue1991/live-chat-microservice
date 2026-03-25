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
