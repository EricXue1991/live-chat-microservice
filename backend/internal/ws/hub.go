package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"livechat/internal/config"
	"livechat/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/redis/go-redis/v9"
)

// Hub manages all WebSocket connections across rooms.
//
// Responsibilities:
//   1. Maintain a map of roomID → set of connected clients
//   2. Handle client register/unregister
//   3. Broadcast messages to all clients in a room
//   4. Consume SQS queue to receive broadcasts from other replicas
//   5. Track online presence in Redis (shared across replicas)
//
// Thread safety: sync.RWMutex protects the rooms map.
type Hub struct {
	id         string // unique replica ID — used to skip self-originated SQS messages
	rooms      map[string]map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan *roomMessage
	mu         sync.RWMutex
	cfg        *config.Config
	sqs        *sqs.Client
	rdb        *redis.Client
}

type roomMessage struct {
	roomID  string
	message []byte
}

func NewHub(cfg *config.Config, sqsClient *sqs.Client, rdb *redis.Client) *Hub {
	return &Hub{
		id:         fmt.Sprintf("hub-%d", time.Now().UnixNano()),
		rooms:      make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *roomMessage, 256),
		cfg:        cfg,
		sqs:        sqsClient,
		rdb:        rdb,
	}
}

// ID returns this hub's unique replica identifier.
func (h *Hub) ID() string { return h.id }

// Run starts the hub's main event loop.
// Also launches the SQS consumer goroutine for cross-replica messages.
func (h *Hub) Run() {
	// Start SQS consumer for cross-replica broadcast
	if h.cfg.BroadcastQueueURL != "" {
		go h.consumeSQS()
	}

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if h.rooms[client.roomID] == nil {
				h.rooms[client.roomID] = make(map[*Client]bool)
			}
			h.rooms[client.roomID][client] = true
			count := len(h.rooms[client.roomID])
			h.mu.Unlock()

			// Update online count in Redis (shared across replicas)
			h.updatePresence(client.roomID, 1)

			log.Printf("[WS] client joined room=%s (local=%d)", client.roomID, count)

			// Send welcome message with online count
			welcome := models.WSMessage{
				Type: "system",
				Payload: map[string]interface{}{
					"message": "Connected to room " + client.roomID,
					"online":  h.getOnlineCount(client.roomID),
				},
			}
			data, _ := json.Marshal(welcome)
			client.send <- data

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.rooms[client.roomID]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					close(client.send)
					if len(clients) == 0 {
						delete(h.rooms, client.roomID)
					}
				}
			}
			h.mu.Unlock()

			h.updatePresence(client.roomID, -1)
			log.Printf("[WS] client left room=%s", client.roomID)

		case msg := <-h.broadcast:
			h.mu.RLock()
			clients := h.rooms[msg.roomID]
			h.mu.RUnlock()

			for client := range clients {
				select {
				case client.send <- msg.message:
				default:
					// Send buffer full — disconnect slow client
					h.mu.Lock()
					delete(h.rooms[msg.roomID], client)
					close(client.send)
					h.mu.Unlock()
				}
			}
		}
	}
}

// BroadcastToRoom sends a message to all clients in a room.
func (h *Hub) BroadcastToRoom(roomID string, msgType string, payload interface{}) {
	wsMsg := models.WSMessage{Type: msgType, Payload: payload}
	data, err := json.Marshal(wsMsg)
	if err != nil {
		log.Printf("[WS] marshal error: %v", err)
		return
	}
	h.broadcast <- &roomMessage{roomID: roomID, message: data}
}

// consumeSQS polls the broadcast queue for messages from other replicas.
//
// Cross-replica flow:
//   User on replica-1 sends message
//   → replica-1 writes to DynamoDB + publishes to SNS
//   → SNS fans out to all replicas' SQS queues
//   → each replica's consumeSQS picks up the message
//   → pushes to local WebSocket clients
func (h *Hub) consumeSQS() {
	log.Println("[WS] starting SQS broadcast consumer")

	for {
		result, err := h.sqs.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(h.cfg.BroadcastQueueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // long polling
		})
		if err != nil {
			log.Printf("[WS] SQS receive error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, sqsMsg := range result.Messages {
			// Unwrap SNS envelope if present
			var snsWrapper struct{ Message string `json:"Message"` }
			body := *sqsMsg.Body
			if json.Unmarshal([]byte(body), &snsWrapper) == nil && snsWrapper.Message != "" {
				body = snsWrapper.Message
			}

			var broadcast models.BroadcastMessage
			if json.Unmarshal([]byte(body), &broadcast) != nil {
				continue
			}

			// Skip messages originating from this replica — already delivered
			// directly to local WebSocket clients via BroadcastToRoom().
			if broadcast.SourceID == h.id {
				h.sqs.DeleteMessage(context.TODO(), &sqs.DeleteMessageInput{
					QueueUrl:      aws.String(h.cfg.BroadcastQueueURL),
					ReceiptHandle: sqsMsg.ReceiptHandle,
				})
				continue
			}

			switch broadcast.Type {
			case "chat":
				if broadcast.Message != nil {
					h.BroadcastToRoom(broadcast.RoomID, "chat", broadcast.Message)
				}
			case "reaction_update":
				if broadcast.Reaction != nil {
					h.BroadcastToRoom(broadcast.RoomID, "reaction", broadcast.Reaction)
				}
			}

			// Acknowledge message
			h.sqs.DeleteMessage(context.TODO(), &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(h.cfg.BroadcastQueueURL),
				ReceiptHandle: sqsMsg.ReceiptHandle,
			})
		}
	}
}

// updatePresence increments/decrements the online count in Redis.
// This gives a global view across all replicas.
func (h *Hub) updatePresence(roomID string, delta int64) {
	if h.rdb == nil {
		return
	}
	key := "online:" + roomID
	ctx := context.Background()
	h.rdb.IncrBy(ctx, key, delta)
	h.rdb.Expire(ctx, key, 5*time.Minute)
}

// getOnlineCount returns the global online count from Redis,
// falling back to local count if Redis is unavailable.
func (h *Hub) getOnlineCount(roomID string) int {
	if h.rdb != nil {
		val, err := h.rdb.Get(context.Background(), "online:"+roomID).Int()
		if err == nil && val > 0 {
			return val
		}
	}
	// Fallback to local count
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[roomID])
}

// GetAllRoomCounts returns online counts for all rooms (for /api/status).
func (h *Hub) GetAllRoomCounts() map[string]int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	counts := make(map[string]int)
	for roomID, clients := range h.rooms {
		counts[roomID] = len(clients)
	}
	return counts
}
