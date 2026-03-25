package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"livechat/internal/config"
	"livechat/internal/models"

	kafkaGo "github.com/segmentio/kafka-go"
)

// Consumer reads events from the Kafka topic and maintains real-time analytics.
//
// Why Kafka (instead of just SQS):
//   - Durable event log: events can be replayed for debugging or reprocessing
//   - Multiple consumer groups: analytics, search indexing, audit logging
//     can each read the same stream independently
//   - Ordered within partition: messages keyed by roomID land in the same
//     partition, preserving per-room ordering
//   - High throughput: Kafka handles millions of events/sec vs SQS's per-queue limits
//
// The consumer tracks per-room statistics in memory and exposes them via GetStats().
type Consumer struct {
	reader *kafkaGo.Reader
	cfg    *config.Config

	// In-memory analytics (production would write to a time-series DB)
	mu    sync.RWMutex
	stats map[string]*RoomStats
}

// RoomStats holds real-time analytics for a single room.
type RoomStats struct {
	RoomID        string         `json:"room_id"`
	MessageCount  int64          `json:"message_count"`
	ReactionCount int64          `json:"reaction_count"`
	UniqueUsers   map[string]bool `json:"unique_users"`
	UserCount     int            `json:"user_count"`
	LastActivity  int64          `json:"last_activity"`
	TopReactions  map[string]int64 `json:"top_reactions"`
}

// NewConsumer creates a Kafka consumer for the livechat-events topic.
func NewConsumer(cfg *config.Config) *Consumer {
	reader := kafkaGo.NewReader(kafkaGo.ReaderConfig{
		Brokers:     []string{cfg.KafkaBrokers},
		Topic:       cfg.KafkaTopic,
		GroupID:     "livechat-analytics",     // consumer group
		MinBytes:    1e3,                       // 1KB min fetch
		MaxBytes:    10e6,                      // 10MB max fetch
		StartOffset: kafkaGo.LastOffset,        // start from latest
		MaxWait:     3 * time.Second,
	})

	return &Consumer{
		reader: reader,
		cfg:    cfg,
		stats:  make(map[string]*RoomStats),
	}
}

// Start runs the consumer loop (blocking). Call in a goroutine.
func (c *Consumer) Start() {
	log.Println("[ANALYTICS] starting Kafka consumer")

	for {
		msg, err := c.reader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("[ANALYTICS] Kafka read error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		var event models.KafkaEvent
		if json.Unmarshal(msg.Value, &event) != nil {
			continue
		}

		c.processEvent(&event)
	}
}

// processEvent updates in-memory analytics based on the event type.
func (c *Consumer) processEvent(event *models.KafkaEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	room := c.stats[event.RoomID]
	if room == nil {
		room = &RoomStats{
			RoomID:       event.RoomID,
			UniqueUsers:  make(map[string]bool),
			TopReactions: make(map[string]int64),
		}
		c.stats[event.RoomID] = room
	}

	room.LastActivity = event.Timestamp
	room.UniqueUsers[event.Username] = true
	room.UserCount = len(room.UniqueUsers)

	switch event.EventType {
	case "message_sent":
		room.MessageCount++
	case "reaction":
		room.ReactionCount++
		// Track per-type reaction counts
		if data, ok := event.Data.(map[string]interface{}); ok {
			if rt, ok := data["reaction_type"].(string); ok {
				room.TopReactions[rt]++
			}
		}
	case "user_joined":
		// Already tracked via UniqueUsers
	}
}

// GetStats returns current analytics for all rooms.
func (c *Consumer) GetStats() map[string]*RoomStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[string]*RoomStats)
	for k, v := range c.stats {
		copied := *v
		copied.UniqueUsers = nil // don't expose full user set
		result[k] = &copied
	}
	return result
}

// GetRoomStats returns analytics for a specific room.
func (c *Consumer) GetRoomStats(roomID string) *RoomStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if s, ok := c.stats[roomID]; ok {
		copied := *s
		copied.UniqueUsers = nil
		return &copied
	}
	return &RoomStats{
		RoomID:       roomID,
		TopReactions: make(map[string]int64),
	}
}

// Stop gracefully shuts down the Kafka consumer.
func (c *Consumer) Stop() {
	if c.reader != nil {
		c.reader.Close()
	}
	fmt.Println("[ANALYTICS] Kafka consumer stopped")
}
