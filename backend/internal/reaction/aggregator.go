package reaction

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"livechat/internal/config"
	"livechat/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// Aggregator is an SQS consumer that batch-updates DynamoDB counters.
//
// Design rationale:
//   Reactions are high-frequency, low-value writes (rapid emoji clicks).
//   Writing to DynamoDB on every click would hit throughput limits fast.
//   Instead, events queue in SQS; the aggregator pulls batches, merges
//   counts in memory, then issues one UpdateItem per (room, reactionType).
//
//   Example: 100 "like" events → 1 DynamoDB UpdateItem (ADD count 100)
//
// Fault tolerance:
//   SQS provides durability. If the aggregator crashes, events remain
//   in the queue and are processed after recovery. No data loss.
type Aggregator struct {
	db  *dynamodb.Client
	sqs *sqs.Client
	cfg *config.Config
}

func NewAggregator(db *dynamodb.Client, sqsClient *sqs.Client, cfg *config.Config) *Aggregator {
	return &Aggregator{db: db, sqs: sqsClient, cfg: cfg}
}

// Start runs the aggregator's main loop (blocking).
func (a *Aggregator) Start() {
	if a.cfg.ReactionQueueURL == "" {
		log.Println("[AGGREGATOR] no reaction queue URL configured, skipping")
		return
	}
	log.Println("[AGGREGATOR] starting reaction aggregator worker")

	for {
		// Pull up to 10 messages from SQS (long polling, 20s wait)
		result, err := a.sqs.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(a.cfg.ReactionQueueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
			VisibilityTimeout:   30,
		})
		if err != nil {
			log.Printf("[AGGREGATOR] SQS receive error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if len(result.Messages) == 0 {
			continue
		}

		// Aggregate counts in memory: "roomId#reactionType" → count
		counts := make(map[string]int64)
		var processed []sqsTypes.DeleteMessageBatchRequestEntry

		for i, sqsMsg := range result.Messages {
			var event models.ReactionEvent
			if json.Unmarshal([]byte(*sqsMsg.Body), &event) != nil {
				continue
			}
			key := event.RoomID + "#" + event.ReactionType
			counts[key]++
			processed = append(processed, sqsTypes.DeleteMessageBatchRequestEntry{
				Id:            aws.String(fmt.Sprintf("msg-%d", i)),
				ReceiptHandle: sqsMsg.ReceiptHandle,
			})
		}

		// Batch update DynamoDB — one write per unique (room, reactionType)
		for key, count := range counts {
			roomID, reactionType := splitKey(key)
			_, err := a.db.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
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
			})
			if err != nil {
				log.Printf("[AGGREGATOR] DynamoDB update error for %s: %v", key, err)
			} else {
				log.Printf("[AGGREGATOR] updated %s: +%d", key, count)
			}
		}

		// Acknowledge processed messages
		if len(processed) > 0 {
			a.sqs.DeleteMessageBatch(context.TODO(), &sqs.DeleteMessageBatchInput{
				QueueUrl: aws.String(a.cfg.ReactionQueueURL),
				Entries:  processed,
			})
		}
	}
}

// splitKey splits "roomId#reactionType" back into its components.
func splitKey(key string) (string, string) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '#' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
