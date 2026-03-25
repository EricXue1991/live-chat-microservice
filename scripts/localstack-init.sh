#!/bin/bash
# LocalStack init — creates all AWS resources on startup.
echo "Initializing LocalStack resources..."

REGION="us-east-1"

# DynamoDB: Messages table (PK=room_id, SK=sort_key)
awslocal dynamodb create-table \
  --table-name livechat-messages \
  --attribute-definitions \
    AttributeName=room_id,AttributeType=S \
    AttributeName=sort_key,AttributeType=S \
  --key-schema \
    AttributeName=room_id,KeyType=HASH \
    AttributeName=sort_key,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST \
  --region ${REGION}

# DynamoDB: Reactions table (PK=room_id, SK=reaction_type)
awslocal dynamodb create-table \
  --table-name livechat-reactions \
  --attribute-definitions \
    AttributeName=room_id,AttributeType=S \
    AttributeName=reaction_type,AttributeType=S \
  --key-schema \
    AttributeName=room_id,KeyType=HASH \
    AttributeName=reaction_type,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST \
  --region ${REGION}

# S3 bucket for attachments
awslocal s3 mb s3://livechat-attachments --region ${REGION}

# SNS topic for cross-replica broadcast
awslocal sns create-topic --name livechat-broadcast --region ${REGION}

# SQS queues
awslocal sqs create-queue --queue-name livechat-reactions \
  --attributes VisibilityTimeout=30,ReceiveMessageWaitTimeSeconds=20 \
  --region ${REGION}

awslocal sqs create-queue --queue-name livechat-broadcast \
  --attributes VisibilityTimeout=10,ReceiveMessageWaitTimeSeconds=20 \
  --region ${REGION}

# Subscribe broadcast queue to SNS topic
awslocal sns subscribe \
  --topic-arn arn:aws:sns:us-east-1:000000000000:livechat-broadcast \
  --protocol sqs \
  --notification-endpoint arn:aws:sqs:us-east-1:000000000000:livechat-broadcast \
  --region ${REGION}

echo "========================================="
echo "  LocalStack resources created!"
echo "  DynamoDB: livechat-messages, livechat-reactions"
echo "  S3:       livechat-attachments"
echo "  SNS:      livechat-broadcast"
echo "  SQS:      livechat-reactions, livechat-broadcast"
echo "========================================="
