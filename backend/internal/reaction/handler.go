package reaction

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"livechat/internal/config"
	"livechat/internal/middleware"
	"livechat/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/redis/go-redis/v9"
	kafkaGo "github.com/segmentio/kafka-go"
)

// Handler processes reaction HTTP requests.
// Two modes (toggled via REACTION_MODE env var for experiment 3):
//   "sync":  each reaction directly updates DynamoDB counter
//   "async": reaction enqueued to SQS, aggregator batch-updates DynamoDB
type Handler struct {
	db    *dynamodb.Client
	sqs   *sqs.Client
	rdb   *redis.Client
	kafka *kafkaGo.Writer
	cfg   *config.Config
}

func NewHandler(db *dynamodb.Client, sqsClient *sqs.Client, rdb *redis.Client, kafka *kafkaGo.Writer, cfg *config.Config) *Handler {
	return &Handler{db: db, sqs: sqsClient, rdb: rdb, kafka: kafka, cfg: cfg}
}

// SubmitReaction handles POST /api/reactions.
// Routes to sync or async path based on REACTION_MODE.
func (h *Handler) SubmitReaction(w http.ResponseWriter, r *http.Request) {
	username := middleware.GetUsername(r)

	var event models.ReactionEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body"})
		return
	}
	if event.RoomID == "" || event.ReactionType == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "room_id and reaction_type required"})
		return
	}

	event.Username = username
	event.Timestamp = models.NowMillis()

	if h.cfg.ReactionMode == "sync" {
		// Sync: direct DynamoDB write per click (experiment 3 baseline)
		h.syncWrite(&event)
	} else {
		// Async: enqueue to SQS for batch aggregation (experiment 3 improved)
		h.asyncEnqueue(&event)
	}

	// Publish to Kafka event stream regardless of mode
	h.publishToKafka(&event)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
		"mode":   h.cfg.ReactionMode,
	})
}

// syncWrite performs an atomic counter increment directly on DynamoDB.
// Simple but becomes a bottleneck under high concurrency.
func (h *Handler) syncWrite(event *models.ReactionEvent) {
	_, err := h.db.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
		TableName: aws.String(h.cfg.ReactionsTable),
		Key: map[string]types.AttributeValue{
			"room_id":       &types.AttributeValueMemberS{Value: event.RoomID},
			"reaction_type": &types.AttributeValueMemberS{Value: event.ReactionType},
		},
		UpdateExpression: aws.String("ADD #count :inc"),
		ExpressionAttributeNames: map[string]string{
			"#count": "count", // "count" is a DynamoDB reserved word
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":inc": &types.AttributeValueMemberN{Value: "1"},
		},
	})
	if err != nil {
		log.Printf("[REACTION] sync write error: %v", err)
	}
}

// asyncEnqueue sends the reaction event to SQS for batch processing.
func (h *Handler) asyncEnqueue(event *models.ReactionEvent) {
	data, _ := json.Marshal(event)
	_, err := h.sqs.SendMessage(context.TODO(), &sqs.SendMessageInput{
		QueueUrl:    aws.String(h.cfg.ReactionQueueURL),
		MessageBody: aws.String(string(data)),
	})
	if err != nil {
		log.Printf("[REACTION] SQS send error: %v", err)
	}
}

// publishToKafka sends the reaction event to Kafka for analytics.
func (h *Handler) publishToKafka(event *models.ReactionEvent) {
	if h.kafka == nil {
		return
	}
	kafkaEvent := models.KafkaEvent{
		EventType: "reaction",
		RoomID:    event.RoomID,
		Username:  event.Username,
		Timestamp: event.Timestamp,
		Data:      event,
	}
	payload, _ := json.Marshal(kafkaEvent)
	h.kafka.WriteMessages(context.Background(), kafkaGo.Message{
		Key:   []byte(event.RoomID),
		Value: payload,
	})
}

// GetReactions handles GET /api/reactions?roomId=xxx.
// Returns all reaction type counts for a room.
// Uses Redis cache if enabled (experiment 5).
func (h *Handler) GetReactions(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("roomId")
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "roomId required"})
		return
	}

	// Try Redis cache first
	if h.cfg.CacheEnabled && h.rdb != nil {
		cached, err := h.rdb.Get(context.Background(), "reactions:"+roomID).Result()
		if err == nil {
			var reactions []models.Reaction
			if json.Unmarshal([]byte(cached), &reactions) == nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"room_id":   roomID,
					"reactions": reactions,
					"source":    "cache",
				})
				return
			}
		}
	}

	// Cache miss — query DynamoDB
	result, err := h.db.Query(context.TODO(), &dynamodb.QueryInput{
		TableName:              aws.String(h.cfg.ReactionsTable),
		KeyConditionExpression: aws.String("room_id = :rid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":rid": &types.AttributeValueMemberS{Value: roomID},
		},
	})
	if err != nil {
		log.Printf("[REACTION] DynamoDB query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "query failed"})
		return
	}

	var reactions []models.Reaction
	attributevalue.UnmarshalListOfMaps(result.Items, &reactions)
	if reactions == nil {
		reactions = []models.Reaction{}
	}

	// Fill cache (short TTL because reactions update frequently)
	if h.cfg.CacheEnabled && h.rdb != nil {
		data, _ := json.Marshal(reactions)
		h.rdb.Set(context.Background(), "reactions:"+roomID, string(data), 3*1000000000) // 3 seconds
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"room_id":   roomID,
		"reactions": reactions,
		"source":    "dynamodb",
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
