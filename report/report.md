# LiveChat Microservice — Project Report

---

## Problem, Team, and Overview of Experiments

### Problem

Real-time communication is a foundational requirement for modern collaborative applications — chat platforms, multiplayer games, live dashboards, and collaborative editing tools all depend on low-latency message delivery at scale.
The core engineering challenge is: **how do you deliver messages to thousands of concurrent users with minimal latency, while remaining horizontally scalable?**

Two competing approaches exist:
- **HTTP Polling**: clients repeatedly ask the server "do you have anything new?"
- **WebSocket push**: the server proactively delivers messages the moment they
  arrive

Beyond delivery transport, a production-grade chat system must handle
authentication, message persistence, caching, rate limiting, and cross-replica
fan-out — each of which introduces measurable trade-offs.

This project builds a production-inspired distributed chat system and runs six
controlled experiments to quantify those trade-offs empirically.

### Team

| Member | Responsibilities |
|--------|-----------------|
| **Yumeng Zeng** | JWT authentication system, HTTP + WebSocket chat infrastructure, Experiment 4 (WebSocket vs HTTP Polling latency) |

### Overview of Experiments

| # | Experiment | What it measures |
|---|-----------|-----------------|
| 1 | Scale-Out | Throughput scaling as replicas increase from 1 → 8 |
| 2 | Hot Room vs Multi-Room | DynamoDB partition hotspot impact on latency |
| 3 | Sync vs Async Reactions | SQS batch aggregation benefit under high reaction load |
| 4 | WebSocket vs HTTP Polling | Push vs pull end-to-end message delivery latency |
| 5 | Cache Hit vs Miss | Redis cache impact on read latency and DynamoDB load |
| 6 | Rate Limiting | System stability under abusive request patterns |

### Role of AI

Claude Code (Anthropic) was used throughout this project to:
- Audit existing code and identify missing components
- Generate experiment tooling (run scripts, docker-compose overrides, plot scripts)
- Diagnose runtime bugs by tracing data flow through the architecture
- Accelerate report writing by generating structured drafts for review

AI was treated as a **pair programmer and architecture reviewer**, not as a code generator operating without human oversight. Every suggestion was reviewed, tested, and validated before being committed.

### Observability

The system exposes:
- `GET /health` — liveness check
- `GET /api/status` — runtime state (reaction queue depth, room counts, cache
  enabled, rate limit config)
- Kafka event stream — durable append-only log of all events for analytics replay
- Redis presence counters — real-time online user counts per room
- Locust CSV output — per-experiment latency percentiles (p50/p95/p99) and
  throughput

---

## Project Plan and Recent Progress

### Timeline （Need to be done）

### AI Cost/Benefit Analysis

**Benefits:**
- Reduced time to identify the SQS latency bug from potentially hours to minutes
  by tracing the exact data flow path through 5 files simultaneously
- Generated experiment scaffolding (3 files) consistent with the existing
  Experiment 3 pattern without manual study of every convention
- Produced structured report drafts that accelerated writing without replacing
  engineering judgment

**Costs / Risks:**
- AI-generated code requires careful review; initial `PollingUser` class had a
  missing `send_message` task, causing the first experiment run to produce no
  latency data
- AI explanations can be confidently wrong — always verify against actual code
  and runtime behavior

---

## Objectives

### Short-Term (within this course)

1. Deliver a fully working distributed chat system runnable locally via Docker
   Compose with a single command
2. Complete all six experiments with statistically meaningful results
3. Demonstrate measurable performance differences across each trade-off axis
4. Document all findings in a reproducible, peer-reviewable format

### Long-Term (beyond this course)

1. **Replace SQS broadcast with Redis Pub/Sub** for sub-millisecond cross-replica
   delivery — the current SNS/SQS approach adds up to 20s latency even when
   fixed, which is acceptable for experiments but not production
2. **Add Prometheus + Grafana** for live metrics dashboards — currently
   observability is post-hoc (CSV files), not real-time
3. **Deploy on ECS with auto-scaling** to run experiments at production scale
   (1000+ concurrent users) rather than local Docker
4. **Add structured logging** (JSON logs with request IDs) to enable distributed
   tracing across replicas

### Observability Roadmap

```
Current:   Locust CSV → manual plot scripts
Next:      Prometheus metrics endpoint → Grafana live dashboard
Future:    OpenTelemetry distributed tracing across all services
```

---

## Related Work

### Course Readings
- **CAP Theorem** — the system prioritizes Availability + Partition Tolerance
  (AP) for the message path; DynamoDB writes succeed even if some replicas are
  behind, making the system eventually consistent

### External References

- **Discord Engineering Blog** — "How Discord Stores Billions of Messages"
  describes a similar DynamoDB + Cassandra hybrid approach for message storage
- **Slack Engineering** — uses a hybrid of WebSocket + long-polling as fallback,
  acknowledging that WebSocket connections are not always stable
- **RFC 6455** — WebSocket protocol specification; the `gorilla/websocket`
  library used in this project implements this RFC


---

## Methodology

### System Architecture

```
Client (React) ──→ Go API (×N replicas, port 8080)
                        │
          ┌─────────────┼──────────────┬──────────────┐
          ▼             ▼              ▼              ▼
     PostgreSQL     DynamoDB        Redis          S3
   (users, rooms) (messages,    (cache, rate    (files)
                   reactions)    limits,
                                presence)
                        │
               ┌────────┼────────┐
               ▼        ▼        ▼
             SNS/SQS  Kafka    SQS
           (broadcast)(events)(reactions)
```

### Authentication (JWT) — Molly

Stateless JWT tokens are issued on successful login and validated on every
protected endpoint via middleware.

**Design decisions:**
- **Stateless** (no server-side sessions): any replica can validate any token without shared state — critical for horizontal scaling in Experiments 1 & 2
- **HMAC-SHA256** signing: prevents algorithm-confusion attacks (explicit algorithm check in middleware)
- **24-hour expiry**: balances security (short-lived) with user experience (infrequent re-login)

```
POST /api/register  →  bcrypt hash password  →  store in PostgreSQL
POST /api/login     →  verify bcrypt hash    →  issue signed JWT
GET  /api/rooms     →  validate JWT          →  return room list (protected)
```

### HTTP Chat — Molly

`POST /api/messages` write path:
```
1. Validate JWT + rate limit check (Redis sliding window)
2. Write message to DynamoDB (PK=room_id, SK=timestamp#messageId)
3. Append to Redis cache list (capped at 200 messages, 10min TTL)
4. Publish to SNS → SQS for cross-replica WebSocket delivery
5. Publish to Kafka for analytics pipeline
```

`GET /api/messages` read path:
```
1. Try Redis cache (if CACHE_ENABLED=true and no since filter)
2. On cache miss → Query DynamoDB with room_id + since timestamp
3. Fill cache on miss
```

### WebSocket — Molly

Each client opens a persistent WebSocket connection to `/ws/rooms/{roomId}`.
The Hub manages room membership and message fan-out:

```
Client connects → Hub.Register → Redis presence +1
Client sends message → readPump → handleChatMessage → DynamoDB + Hub.Broadcast
Hub.Broadcast → writePump on all clients in room → message delivered
Cross-replica: consumeSQS (WaitTimeSeconds=20) → Hub.BroadcastToRoom
Client disconnects → Hub.Unregister → Redis presence -1
```

### Experiment 4: WebSocket vs HTTP Polling — Molly

**Hypothesis:** WebSocket push delivers messages with significantly lower
end-to-end latency than HTTP polling, because WebSocket eliminates the polling
interval overhead (~500ms average for 1s poll intervals).

**Setup:**
- Single replica (rate limiting disabled via `docker-compose.exp4-overrides.yml`)
- 50 concurrent users, spawn rate 5/s, 90s duration per pass
- Same room (`room-general`) for both passes to ensure comparable message volume

**PollingUser behaviour:**
- Sends a message every ~4 tasks (weight 1)
- Polls `/api/messages?since=last_ts` every ~1s (weight 3)
- Fires `POLL_LATENCY` event: `now_ms - message.timestamp`

**WebSocketUser behaviour:**
- Holds a persistent WebSocket connection via background thread
- Sends messages via HTTP POST every 1-3s
- Fires `WS_LATENCY` event in `_on_msg` callback: `now_ms - payload.timestamp`

**Key design choice:** Both user types send AND receive, making the comparison
symmetric. A `PollingUser` that only polls would find no messages to measure.

---

## Preliminary Results

### Experiment 4 — Pre-Fix Results (Initial Run)

| Metric | HTTP Polling | WebSocket | Speedup |
|--------|-------------|-----------|---------|
| p50 | 1000 ms | 17000 ms | 0.1x ❌ |
| p95 | 3200 ms | 33000 ms | 0.1x ❌ |
| p99 | 4600 ms | 37000 ms | 0.1x ❌ |
| Average | 1261 ms | 17160 ms | 0.1x ❌ |

**WebSocket performed ~17x worse than polling — the opposite of the hypothesis.**

### Root Cause Analysis

Tracing the message delivery path revealed a missing step in `chat/handler.go`:

```
Current (buggy) path for HTTP POST → WebSocket delivery:

POST /api/messages
    │
    ▼
DynamoDB write
    │
    ▼
SNS publish ──→ SQS queue ──→ Hub.consumeSQS()
                                    ↑
                          WaitTimeSeconds: 20
                          (long poll, up to 20s wait)
                                    │
                                    ▼
                          WebSocket clients
                          Average delay: ~10-17s
```

The `chat/handler.go` broadcasts via SNS for cross-replica delivery but **never
calls `hub.BroadcastToRoom()` directly** for same-replica clients. All WebSocket
delivery — even for clients on the same server — goes through the full
SNS → SQS → hub cycle, incurring up to 20 seconds of SQS long-poll latency.

This is an **incomplete implementation**, not a design flaw. The SNS/SQS path
is correctly designed for multi-replica fan-out. The missing piece is the direct
local broadcast path for same-replica clients:

```
Correct path (post-fix):

POST /api/messages
    │
    ├──→ hub.BroadcastToRoom()  ──→ WebSocket clients (~20ms) ✅
    │         (direct, local)
    │
    └──→ SNS → SQS  ──→ Other replicas' hubs  ✅
         (cross-replica, ~20s, acceptable)
```

### Bug Fix: Direct Hub Broadcast

Four files modified to implement the fix:

**1. `models/models.go`** — Add `SourceID` to `BroadcastMessage` to prevent
duplicate delivery (same-replica SQS consumer skips its own messages):
```go
type BroadcastMessage struct {
    Type     string    `json:"type"`
    RoomID   string    `json:"room_id"`
    Message  *Message  `json:"message,omitempty"`
    Reaction *Reaction `json:"reaction,omitempty"`
    SourceID string    `json:"source_id,omitempty"` // added
}
```

**2. `ws/hub.go`** — Add replica ID, expose `ID()`, skip own SQS messages:
```go
type Hub struct {
    id string  // unique per replica instance
    ...
}
// In consumeSQS: skip if broadcast.SourceID == h.id
```

**3. `chat/handler.go`** — Add `Broadcaster` interface + direct broadcast:
```go
type Broadcaster interface {
    BroadcastToRoom(roomID string, msgType string, payload interface{})
}
// In SendMessage: h.hub.BroadcastToRoom(msg.RoomID, "chat", &msg)
```

**4. `cmd/server/main.go`** — Wire hub into chat handler after hub creation:
```go
hub := ws.NewHub(cfg, sqsClient, rdb)
go hub.Run()
chatHandler.SetHub(hub, hub.ID())  // added
```

### Experiment 4 — Post-Fix Results

After implementing direct hub broadcast in `chat/handler.go`, WebSocket
correctly outperforms HTTP polling across all latency percentiles.

| Metric | HTTP Polling | WebSocket | Speedup |
|--------|-------------|-----------|---------|
| p50    | 970 ms      | 180 ms    | **5.4x** |
| p95    | 3300 ms     | 1000 ms   | **3.3x** |
| p99    | 5100 ms     | 2500 ms   | **2.0x** |
| Average | 1236 ms    | 328 ms    | **3.8x** |

**WebSocket p50 (180ms) is higher than the theoretical minimum (~20ms)** because
under 50 concurrent users, the hub's single-threaded event loop processes
messages sequentially — bursts of 50 simultaneous broadcasts queue in the
channel, adding ~160ms of queuing latency on average. In a single-user test,
WebSocket latency would approach ~20ms.

**HTTP Polling p50 (970ms) slightly exceeds the expected ~500ms average**
because under load, DynamoDB and Redis reads add processing time on top of
the polling interval.

The p99 gap (5100ms vs 2500ms) reflects tail latency under contention:
polling users occasionally miss two consecutive poll cycles if the server is
busy, while WebSocket users experience hub channel back-pressure during bursts.

### Engineering Insight

The bug exposes a real architectural consideration: in a **single-replica**
system, routing WebSocket delivery through SNS/SQS adds unnecessary latency.
In a **multi-replica** system, the SQS path is essential for cross-replica
fan-out. Production systems (e.g. Discord, Slack) solve this by using
**Redis Pub/Sub** for cross-replica broadcast instead of SQS — Redis delivers
pub/sub messages in ~1ms, making the distinction between local and remote
delivery negligible.

### Worst-Case Workload

For Experiment 4, the worst case for **HTTP polling** is high message rate with
many concurrent pollers: every poll hits Redis/DynamoDB even when no new
messages exist, creating wasted read operations. For **WebSocket**, the worst
case is many users in the same room during a burst — the hub fan-out is O(N)
in the number of connected clients per room.

---

## Impact

### Why This Work Matters

Real-time messaging is one of the most common distributed systems requirements,
yet the performance trade-offs between delivery models are rarely quantified
empirically. This project provides:

1. **Concrete latency numbers** comparing WebSocket push vs HTTP polling under
   realistic concurrent load — useful for teams deciding which transport to use
2. **A reproducible benchmark environment** — the full stack runs with a single
   `docker compose up`, enabling others to replicate or extend the experiments
3. **A working example of common pitfalls** — the SQS latency bug demonstrates
   how an architecturally correct multi-replica design can silently degrade
   single-replica performance, a subtle issue not documented in most tutorials

### Reproducibility

Anyone in the class can clone the repository and run all experiments locally:
```bash
git clone https://github.com/EricXue1991/live-chat-microservice.git
docker compose up --build
# Experiments 1-6 each have a run script in scripts/
```

No AWS account or cloud infrastructure required — all AWS services run locally
via LocalStack.

### Future Work

- Replace SQS broadcast with Redis Pub/Sub to bring cross-replica WebSocket
  latency from ~20s to ~1ms
- Add WebSocket reconnection logic and measure impact on latency during
  transient network failures
- Scale to 1000+ concurrent users on ECS to validate whether the latency
  advantages hold under production-scale load
