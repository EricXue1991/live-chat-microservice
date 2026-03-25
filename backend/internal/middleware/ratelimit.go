package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a sliding-window rate limiter using Redis.
// Each user gets N requests per second. Exceeding the limit returns 429.
//
// Why Redis for rate limiting:
//   - Shared state across all replicas (unlike in-memory counters)
//   - Atomic INCR + EXPIRE guarantees correctness under concurrency
//   - Sub-millisecond latency — negligible overhead per request
//
// Experiment 6: toggle rate limiting on/off and measure system
// stability under abusive load patterns (e.g., one user sending
// hundreds of messages per second).
type RateLimiter struct {
	rdb        *redis.Client
	rps        int // max requests per second per user
	enabled    bool
}

// NewRateLimiter creates a rate limiter.
// Set rps=0 or enabled=false to disable.
func NewRateLimiter(rdb *redis.Client, rps int, enabled bool) *RateLimiter {
	return &RateLimiter{rdb: rdb, rps: rps, enabled: enabled}
}

// Middleware returns an HTTP middleware that enforces the rate limit.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.enabled || rl.rps <= 0 || rl.rdb == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Identify user by JWT username (already in context), fall back to IP
			username := GetUsername(r)
			if username == "" {
				username = r.RemoteAddr
			}

			// Redis key: "rl:{username}:{current_second}"
			// This implements a per-second sliding window
			now := time.Now().Unix()
			key := fmt.Sprintf("rl:%s:%d", username, now)

			ctx := context.Background()

			// INCR atomically increments; first call creates the key with value 1
			count, err := rl.rdb.Incr(ctx, key).Result()
			if err != nil {
				// Redis down — fail open (allow request) to avoid cascading failures
				next.ServeHTTP(w, r)
				return
			}

			// Set TTL on first increment so keys auto-expire
			if count == 1 {
				rl.rdb.Expire(ctx, key, 2*time.Second)
			}

			if int(count) > rl.rps {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
