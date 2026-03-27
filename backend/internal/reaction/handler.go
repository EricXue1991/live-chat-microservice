package reaction

import (
	"context"
	"encoding/json"
	"fmt"
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

// Handler serves HTTP endpoints for reactions.
// Redis is reserved for optional cache/presence (same wiring as chat); Kafka is optional analytics.
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

// SubmitReaction accepts a single reaction event.
// POST /api/reactions
//
// Modes (REACTION_MODE env, for experiment 3):
//
//	"sync":  write directly to DynamoDB
//	"async": enqueue to SQS for the aggregator worker to batch-flush
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

	ctx := r.Context()

	// Pick sync vs async persistence based on config.
	var err error
	if h.cfg.ReactionMode == "sync" {
		// Sync: one DynamoDB write per reaction (simple; can bottleneck under load).
		err = h.syncWrite(ctx, &event)
	} else {
		// Async: enqueue to SQS; aggregator batches writes and decouples API from storage.
		err = h.asyncEnqueue(ctx, &event)
	}
	if err != nil {
		log.Printf("[REACTION] submit failed (mode=%s): %v", h.cfg.ReactionMode, err)
		writeJSON(w, http.StatusServiceUnavailable, models.ErrorResponse{
			Error: "failed to persist reaction",
		})
		return
	}

	h.publishReactionToKafka(&event)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
		"mode":   h.cfg.ReactionMode,
	})
}

// syncWrite increments the reaction counter in DynamoDB (UpdateItem ADD).
// Experiment 3 baseline: one DynamoDB write per reaction.
func (h *Handler) syncWrite(ctx context.Context, event *models.ReactionEvent) error {
	_, err := h.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(h.cfg.ReactionsTable),
		Key: map[string]types.AttributeValue{
			"room_id":       &types.AttributeValueMemberS{Value: event.RoomID},
			"reaction_type": &types.AttributeValueMemberS{Value: event.ReactionType},
		},
		UpdateExpression: aws.String("ADD #count :inc"),
		ExpressionAttributeNames: map[string]string{
			"#count": "count", // "count" is reserved; use an expression attribute name
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":inc": &types.AttributeValueMemberN{Value: "1"},
		},
	})
	if err != nil {
		log.Printf("[REACTION] sync DynamoDB UpdateItem error: %v", err)
	}
	return err
}

// asyncEnqueue sends the reaction event to SQS for the aggregator to consume in batches.
func (h *Handler) asyncEnqueue(ctx context.Context, event *models.ReactionEvent) error {
	if h.cfg.ReactionQueueURL == "" {
		return fmt.Errorf("SQS_REACTION_QUEUE_URL is not set")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	_, err = h.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(h.cfg.ReactionQueueURL),
		MessageBody: aws.String(string(data)),
	})
	if err != nil {
		log.Printf("[REACTION] SQS send error: %v", err)
	}
	return err
}

// publishReactionToKafka emits a durable event for the analytics pipeline (optional; nil writer = no-op).
func (h *Handler) publishReactionToKafka(event *models.ReactionEvent) {
	if h.kafka == nil {
		return
	}
	ke := models.KafkaEvent{
		EventType: "reaction",
		RoomID:    event.RoomID,
		Username:  event.Username,
		Timestamp: event.Timestamp,
		Data: map[string]string{
			"reaction_type": event.ReactionType,
			"mode":          h.cfg.ReactionMode,
		},
	}
	payload, err := json.Marshal(ke)
	if err != nil {
		return
	}
	if err := h.kafka.WriteMessages(context.Background(), kafkaGo.Message{
		Key:   []byte(event.RoomID),
		Value: payload,
	}); err != nil {
		log.Printf("[REACTION] Kafka publish error: %v", err)
	}
}

// GetReactions returns per-type counts for a room.
// GET /api/reactions?roomId=xxx
func (h *Handler) GetReactions(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("roomId")
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "roomId query parameter required"})
		return
	}

	// Query all reaction types and counts for this partition (room_id).
	result, err := h.db.Query(r.Context(), &dynamodb.QueryInput{
		TableName:              aws.String(h.cfg.ReactionsTable),
		KeyConditionExpression: aws.String("room_id = :rid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":rid": &types.AttributeValueMemberS{Value: roomID},
		},
	})
	if err != nil {
		log.Printf("[REACTION] DynamoDB Query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "failed to query reactions"})
		return
	}

	var reactions []models.Reaction
	if err := attributevalue.UnmarshalListOfMaps(result.Items, &reactions); err != nil {
		log.Printf("[REACTION] unmarshal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "internal error"})
		return
	}

	if reactions == nil {
		reactions = []models.Reaction{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"room_id":   roomID,
		"reactions": reactions,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
