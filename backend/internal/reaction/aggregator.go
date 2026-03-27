package reaction

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"livechat/internal/config"
	"livechat/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// Aggregator is an SQS consumer that batches reaction events into fewer DynamoDB writes.
//
// Rationale: reactions are high-frequency, low-value events (e.g. rapid emoji taps).
// Writing each tap to DynamoDB directly can hit write limits quickly. The aggregator
// pulls batches from SQS, combines counts in memory, then applies one ADD per
// (room_id, reaction_type)—e.g. 100 "like" events → one UpdateItem (ADD 100).
//
// Resilience: SQS persists messages; if the worker dies, messages become visible again
// and processing resumes without losing undelivered work.
type Aggregator struct {
	db  *dynamodb.Client
	sqs *sqs.Client
	cfg *config.Config
}

func NewAggregator(db *dynamodb.Client, sqsClient *sqs.Client, cfg *config.Config) *Aggregator {
	return &Aggregator{db: db, sqs: sqsClient, cfg: cfg}
}

// Start runs the aggregator receive → aggregate → flush loop until process exits.
func (a *Aggregator) Start() {
	if a.cfg.ReactionQueueURL == "" {
		log.Println("[AGGREGATOR] No reaction queue URL configured, skipping")
		return
	}

	log.Println("[AGGREGATOR] Starting reaction aggregator worker")

	for {
		// Receive up to 10 messages per poll (SQS batch limit).
		result, err := a.sqs.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(a.cfg.ReactionQueueURL),
			MaxNumberOfMessages: 10, // max per ReceiveMessage call
			WaitTimeSeconds:     20, // long polling
			VisibilityTimeout:   30, // allow time to flush before redelivery
		})
		if err != nil {
			log.Printf("[AGGREGATOR] SQS receive error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(result.Messages) == 0 {
			continue
		}

		// Aggregate in memory: key "roomId#reactionType" -> delta count for this batch.
		counts := make(map[string]int64)
		var processed []sqsTypes.DeleteMessageBatchRequestEntry

		for i, sqsMsg := range result.Messages {
			var event models.ReactionEvent
			if err := json.Unmarshal([]byte(*sqsMsg.Body), &event); err != nil {
				log.Printf("[AGGREGATOR] unmarshal error: %v", err)
				continue
			}
			if event.RoomID == "" || event.ReactionType == "" {
				log.Printf("[AGGREGATOR] skip event with empty room_id or reaction_type")
				continue
			}

			// Merge counts for the same room + reactionType.
			key := event.RoomID + "#" + event.ReactionType
			counts[key]++

			// Track SQS receipts to delete after a successful flush.
			id := fmt.Sprintf("msg-%d", i)
			if sqsMsg.MessageId != nil {
				id = *sqsMsg.MessageId
			}
			processed = append(processed, sqsTypes.DeleteMessageBatchRequestEntry{
				Id:            aws.String(id),
				ReceiptHandle: sqsMsg.ReceiptHandle,
			})
		}

		// Flush to DynamoDB in a single transaction (all keys succeed or none).
		// Prevents partial writes followed by deleting the whole SQS batch (lost events).
		transactItems := make([]types.TransactWriteItem, 0, len(counts))
		for key, count := range counts {
			roomID, reactionType := splitRoomReactionKey(key)
			if roomID == "" || reactionType == "" {
				log.Printf("[AGGREGATOR] bad aggregate key %q, skipping", key)
				continue
			}
			transactItems = append(transactItems, types.TransactWriteItem{
				Update: &types.Update{
					TableName: aws.String(a.cfg.ReactionsTable),
					Key: map[string]types.AttributeValue{
						"room_id":       &types.AttributeValueMemberS{Value: roomID},
						"reaction_type": &types.AttributeValueMemberS{Value: reactionType},
					},
					UpdateExpression: aws.String("ADD #count :inc"),
					ExpressionAttributeNames: map[string]string{
						"#count": "count",
					},
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":inc": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", count)},
					},
				},
			})
		}

		if len(transactItems) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			_, err := a.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
				TransactItems: transactItems,
			})
			cancel()
			if err != nil {
				log.Printf("[AGGREGATOR] TransactWriteItems error: %v", err)
				continue
			}
			for key, count := range counts {
				log.Printf("[AGGREGATOR] Updated %s: +%d", key, count)
			}
		}

		// Delete the SQS batch only after the transaction commits successfully.
		if len(processed) > 0 && len(transactItems) > 0 {
			_, err := a.sqs.DeleteMessageBatch(context.TODO(), &sqs.DeleteMessageBatchInput{
				QueueUrl: aws.String(a.cfg.ReactionQueueURL),
				Entries:  processed,
			})
			if err != nil {
				log.Printf("[AGGREGATOR] SQS batch delete error: %v", err)
			}
		}
	}
}

// splitRoomReactionKey parses aggregate keys "room_id#reaction_type" (reaction_type must not contain '#').
func splitRoomReactionKey(key string) (roomID, reactionType string) {
	i := strings.LastIndex(key, "#")
	if i <= 0 || i >= len(key)-1 {
		return "", ""
	}
	return key[:i], key[i+1:]
}
