package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"livechat/internal/config"
	"livechat/internal/middleware"
	"livechat/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/redis/go-redis/v9"
	kafkaGo "github.com/segmentio/kafka-go"
)

// Broadcaster is a local WebSocket hub that can push messages to room clients.
// Defined as an interface here to avoid a circular import between chat and ws packages.
type Broadcaster interface {
	BroadcastToRoom(roomID string, msgType string, payload interface{})
}

// Handler processes chat message HTTP requests.
// Write path: client → DynamoDB → direct Hub broadcast → SNS (cross-replica) → Kafka
// Read path:  Redis cache (hit) → DynamoDB (miss) → cache fill
type Handler struct {
	db      *dynamodb.Client
	sns     *sns.Client
	rdb     *redis.Client
	kafka   *kafkaGo.Writer
	cfg     *config.Config
	hub     Broadcaster // direct local WebSocket delivery (set via SetHub)
	hubID   string      // sent as SourceID in SNS so SQS consumer skips own messages
}

func NewHandler(db *dynamodb.Client, snsClient *sns.Client, rdb *redis.Client, kafka *kafkaGo.Writer, cfg *config.Config) *Handler {
	return &Handler{db: db, sns: snsClient, rdb: rdb, kafka: kafka, cfg: cfg}
}

// SetHub wires the local WebSocket hub into the chat handler.
// Must be called after both the handler and hub are initialized in main.go.
func (h *Handler) SetHub(hub Broadcaster, hubID string) {
	h.hub = hub
	h.hubID = hubID
}

// SendMessage handles POST /api/messages.
// 1. Build message with UUID and timestamp
// 2. Write to DynamoDB (primary store)
// 3. Push to Redis cache (append to room's recent-messages list)
// 4. Broadcast via SNS to all replicas
// 5. Publish event to Kafka for analytics pipeline
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	username := middleware.GetUsername(r)

	var req models.SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body"})
		return
	}
	if req.RoomID == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "room_id and content required"})
		return
	}

	// Build message
	msgID := generateID()
	now := models.NowMillis()
	msg := models.Message{
		RoomID:        req.RoomID,
		SortKey:       fmt.Sprintf("%d#%s", now, msgID),
		MessageID:     msgID,
		Username:      username,
		Content:       req.Content,
		AttachmentURL: req.AttachmentURL,
		Timestamp:     now,
		CreatedAt:     models.NowISO(),
	}

	// Write to DynamoDB
	item, _ := attributevalue.MarshalMap(msg)
	_, err := h.db.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(h.cfg.MessagesTable),
		Item:      item,
	})
	if err != nil {
		log.Printf("[CHAT] DynamoDB PutItem error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "failed to save message"})
		return
	}

	// Push to Redis cache — append to room's message list, trim to last 200
	h.cacheMessage(&msg)

	// Direct local broadcast — instantly pushes to WebSocket clients on this replica.
	// This is the fast path (~15ms). The SNS path below handles other replicas.
	if h.hub != nil {
		h.hub.BroadcastToRoom(msg.RoomID, "chat", &msg)
	}

	// Broadcast via SNS to other replicas (SourceID prevents self-redelivery via SQS)
	h.broadcastViaSNS(&msg)

	// Publish to Kafka event stream for analytics
	h.publishToKafka("message_sent", msg.RoomID, msg.Username, msg)

	log.Printf("[CHAT] message sent: room=%s user=%s id=%s", msg.RoomID, msg.Username, msg.MessageID)
	writeJSON(w, http.StatusCreated, msg)
}

// GetMessages handles GET /api/messages?roomId=xxx&since=timestamp&limit=50.
// Read path with Redis cache:
//   1. Try Redis first (cache hit → return immediately)
//   2. On cache miss → query DynamoDB → fill cache
//
// Experiment 5: toggle CACHE_ENABLED to measure latency with/without cache.
func (h *Handler) GetMessages(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("roomId")
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "roomId required"})
		return
	}

	sinceStr := r.URL.Query().Get("since")
	since := int64(0)
	if sinceStr != "" {
		since, _ = strconv.ParseInt(sinceStr, 10, 64)
	}

	limitStr := r.URL.Query().Get("limit")
	limit := int32(50)
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 32); err == nil && l > 0 && l <= 200 {
			limit = int32(l)
		}
	}

	// Try Redis cache first (only when fetching recent messages without a since filter)
	if h.cfg.CacheEnabled && since == 0 && h.rdb != nil {
		cached, err := h.getCachedMessages(roomID, int(limit))
		if err == nil && len(cached) > 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"messages": cached,
				"count":    len(cached),
				"source":   "cache",
			})
			return
		}
	}

	// Cache miss or since filter → query DynamoDB
	input := &dynamodb.QueryInput{
		TableName:              aws.String(h.cfg.MessagesTable),
		KeyConditionExpression: aws.String("room_id = :rid AND sort_key > :since"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":rid":   &types.AttributeValueMemberS{Value: roomID},
			":since": &types.AttributeValueMemberS{Value: fmt.Sprintf("%d", since)},
		},
		ScanIndexForward: aws.Bool(true),
		Limit:            aws.Int32(limit),
	}

	result, err := h.db.Query(context.TODO(), input)
	if err != nil {
		log.Printf("[CHAT] DynamoDB Query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "query failed"})
		return
	}

	var messages []models.Message
	attributevalue.UnmarshalListOfMaps(result.Items, &messages)
	if messages == nil {
		messages = []models.Message{}
	}

	// Fill cache on miss
	if h.cfg.CacheEnabled && since == 0 && len(messages) > 0 {
		h.fillCache(roomID, messages)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
		"source":   "dynamodb",
	})
}

// cacheMessage appends a message to the room's Redis list.
// List is capped at 200 entries with LTRIM.
func (h *Handler) cacheMessage(msg *models.Message) {
	if !h.cfg.CacheEnabled || h.rdb == nil {
		return
	}
	data, _ := json.Marshal(msg)
	key := "msgs:" + msg.RoomID
	ctx := context.Background()

	h.rdb.RPush(ctx, key, string(data))
	h.rdb.LTrim(ctx, key, -200, -1)          // keep last 200
	h.rdb.Expire(ctx, key, 10*time.Minute)    // TTL 10 min
}

// getCachedMessages reads recent messages from Redis.
func (h *Handler) getCachedMessages(roomID string, limit int) ([]models.Message, error) {
	key := "msgs:" + roomID
	ctx := context.Background()

	// Get the last `limit` entries
	start := int64(-1 * limit)
	vals, err := h.rdb.LRange(ctx, key, start, -1).Result()
	if err != nil || len(vals) == 0 {
		return nil, err
	}

	messages := make([]models.Message, 0, len(vals))
	for _, v := range vals {
		var msg models.Message
		if json.Unmarshal([]byte(v), &msg) == nil {
			messages = append(messages, msg)
		}
	}
	return messages, nil
}

// fillCache populates Redis with messages from DynamoDB (on cache miss).
func (h *Handler) fillCache(roomID string, messages []models.Message) {
	if h.rdb == nil {
		return
	}
	key := "msgs:" + roomID
	ctx := context.Background()
	pipe := h.rdb.Pipeline()
	pipe.Del(ctx, key)
	for _, msg := range messages {
		data, _ := json.Marshal(msg)
		pipe.RPush(ctx, key, string(data))
	}
	pipe.Expire(ctx, key, 10*time.Minute)
	pipe.Exec(ctx)
}

// broadcastViaSNS publishes a message to SNS for cross-replica delivery.
func (h *Handler) broadcastViaSNS(msg *models.Message) {
	if h.cfg.SNSTopicARN == "" {
		return
	}
	broadcast := models.BroadcastMessage{Type: "chat", RoomID: msg.RoomID, Message: msg, SourceID: h.hubID}
	data, _ := json.Marshal(broadcast)
	_, err := h.sns.Publish(context.TODO(), &sns.PublishInput{
		TopicArn: aws.String(h.cfg.SNSTopicARN),
		Message:  aws.String(string(data)),
	})
	if err != nil {
		log.Printf("[CHAT] SNS publish error: %v", err)
	}
}

// publishToKafka sends an event to the Kafka topic for analytics.
func (h *Handler) publishToKafka(eventType, roomID, username string, data interface{}) {
	if h.kafka == nil {
		return
	}
	event := models.KafkaEvent{
		EventType: eventType,
		RoomID:    roomID,
		Username:  username,
		Timestamp: models.NowMillis(),
		Data:      data,
	}
	payload, _ := json.Marshal(event)
	h.kafka.WriteMessages(context.Background(), kafkaGo.Message{
		Key:   []byte(roomID),
		Value: payload,
	})
}

// generateID produces a short unique ID using UUID.
func generateID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano()%100000, randomHex(6))
}

func randomHex(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hex[time.Now().UnixNano()%16]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
