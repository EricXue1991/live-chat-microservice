package reaction

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
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// reactionKey is used as a map key to aggregate counts per (room, reaction type).
// Using a struct avoids string concatenation and '#' delimiter ambiguity.
type reactionKey struct {
	roomID       string
	reactionType string
}

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

// Start launches N parallel worker goroutines (AGGREGATOR_WORKERS) and blocks until all exit.
// SQS natively supports multiple consumers — each worker independently polls and flushes.
func (a *Aggregator) Start() {
	if a.cfg.ReactionQueueURL == "" {
		log.Println("[AGGREGATOR] No reaction queue URL configured, skipping")
		return
	}

	workers := a.cfg.AggregatorWorkers
	if workers < 1 {
		workers = 1
	}
	log.Printf("[AGGREGATOR] Starting %d worker(s)", workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			a.runWorker(id)
		}(i)
	}
	wg.Wait()
}

// runWorker is the per-goroutine poll → aggregate → flush loop.
func (a *Aggregator) runWorker(id int) {
	for {
		// Receive up to 10 messages per poll (SQS batch limit).
		result, err := a.sqs.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(a.cfg.ReactionQueueURL),
			MaxNumberOfMessages: 10, // max per ReceiveMessage call
			WaitTimeSeconds:     20, // long polling
			VisibilityTimeout:   30, // allow time to flush before redelivery
		})
		if err != nil {
			log.Printf("[AGGREGATOR:%d] SQS receive error: %v", id, err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(result.Messages) == 0 {
			continue
		}

		// Aggregate in memory: (roomID, reactionType) -> delta count for this batch.
		counts := make(map[reactionKey]int64)
		var validMsgs []sqsTypes.DeleteMessageBatchRequestEntry // successfully parsed, pending DynamoDB flush
		var poisonMsgs []sqsTypes.DeleteMessageBatchRequestEntry // unparseable; delete immediately

		for i, sqsMsg := range result.Messages {
			msgID := fmt.Sprintf("msg-%d", i)
			if sqsMsg.MessageId != nil {
				msgID = *sqsMsg.MessageId
			}
			entry := sqsTypes.DeleteMessageBatchRequestEntry{
				Id:            aws.String(msgID),
				ReceiptHandle: sqsMsg.ReceiptHandle,
			}

			var event models.ReactionEvent
			if err := json.Unmarshal([]byte(*sqsMsg.Body), &event); err != nil {
				log.Printf("[AGGREGATOR:%d] unmarshal error, discarding message %s: %v", id, msgID, err)
				poisonMsgs = append(poisonMsgs, entry)
				continue
			}
			if event.RoomID == "" || event.ReactionType == "" {
				log.Printf("[AGGREGATOR:%d] missing room_id or reaction_type, discarding message %s", id, msgID)
				poisonMsgs = append(poisonMsgs, entry)
				continue
			}

			counts[reactionKey{event.RoomID, event.ReactionType}]++
			validMsgs = append(validMsgs, entry)
		}

		// Discard poison pills immediately — they will never succeed.
		if len(poisonMsgs) > 0 {
			if _, err := a.sqs.DeleteMessageBatch(context.TODO(), &sqs.DeleteMessageBatchInput{
				QueueUrl: aws.String(a.cfg.ReactionQueueURL),
				Entries:  poisonMsgs,
			}); err != nil {
				log.Printf("[AGGREGATOR:%d] SQS poison delete error: %v", id, err)
			}
		}

		if len(counts) == 0 {
			continue
		}

		// Build TransactWriteItem list from aggregated counts.
		var transactItems []types.TransactWriteItem
		for k, count := range counts {
			transactItems = append(transactItems, types.TransactWriteItem{
				Update: &types.Update{
					TableName: aws.String(a.cfg.ReactionsTable),
					Key: map[string]types.AttributeValue{
						"room_id":       &types.AttributeValueMemberS{Value: k.roomID},
						"reaction_type": &types.AttributeValueMemberS{Value: k.reactionType},
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

		// DynamoDB TransactWriteItems limit is 100 items — chunk if needed.
		const maxTransactItems = 100
		failed := false
		for i := 0; i < len(transactItems); i += maxTransactItems {
			end := i + maxTransactItems
			if end > len(transactItems) {
				end = len(transactItems)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			_, err := a.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
				TransactItems: transactItems[i:end],
			})
			cancel()
			if err != nil {
				log.Printf("[AGGREGATOR:%d] TransactWriteItems error (chunk %d-%d): %v", id, i, end, err)
				failed = true
				break
			}
		}
		if failed {
			continue
		}

		for k, count := range counts {
			log.Printf("[AGGREGATOR:%d] Updated %s#%s: +%d", id, k.roomID, k.reactionType, count)
		}

		// Delete valid SQS messages only after all transactions commit successfully.
		if len(validMsgs) > 0 {
			if _, err := a.sqs.DeleteMessageBatch(context.TODO(), &sqs.DeleteMessageBatchInput{
				QueueUrl: aws.String(a.cfg.ReactionQueueURL),
				Entries:  validMsgs,
			}); err != nil {
				log.Printf("[AGGREGATOR:%d] SQS batch delete error: %v", id, err)
			}
		}
	}
}

