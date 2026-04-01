package models

import "time"

// ==========================================================
// User — stored in PostgreSQL (relational data)
// ==========================================================

// User represents a registered account.
// Stored in PostgreSQL because user data has relational properties
// (e.g., room membership, profiles) that benefit from SQL joins.
type User struct {
	ID           int    `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"` // never exposed in JSON
	DisplayName  string `json:"display_name,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// Room represents a chat room.
// Stored in PostgreSQL for relational queries (members, metadata).
type Room struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

// RegisterRequest is the payload for POST /api/register.
type RegisterRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
}

// LoginRequest is the payload for POST /api/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is returned on successful login.
type LoginResponse struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
}

// ==========================================================
// Message — stored in DynamoDB (high-throughput, room-partitioned)
// Key design: PK = room_id, SK = timestamp#message_id
// ==========================================================

type Message struct {
	RoomID        string `json:"room_id" dynamodbav:"room_id"`
	SortKey       string `json:"-" dynamodbav:"sort_key"`               // timestamp#messageId
	MessageID     string `json:"message_id" dynamodbav:"message_id"`
	Username      string `json:"username" dynamodbav:"username"`
	Content       string `json:"content" dynamodbav:"content"`
	AttachmentURL string `json:"attachment_url,omitempty" dynamodbav:"attachment_url"`
	Timestamp     int64  `json:"timestamp" dynamodbav:"timestamp"`
	CreatedAt     string `json:"created_at" dynamodbav:"created_at"`
}

// SendMessageRequest is the payload for POST /api/messages.
type SendMessageRequest struct {
	RoomID        string `json:"room_id"`
	Content       string `json:"content"`
	AttachmentURL string `json:"attachment_url,omitempty"`
}

// ==========================================================
// Reaction — stored in DynamoDB (atomic counter updates)
// Key design: PK = room_id, SK = reaction_type
// ==========================================================

type Reaction struct {
	RoomID       string `json:"room_id" dynamodbav:"room_id"`
	ReactionType string `json:"reaction_type" dynamodbav:"reaction_type"`
	Count        int64  `json:"count" dynamodbav:"count"`
}

// ReactionEvent is a single reaction click, sent to SQS for async aggregation.
type ReactionEvent struct {
	RoomID       string `json:"room_id"`
	ReactionType string `json:"reaction_type"`
	Username     string `json:"username"`
	Timestamp    int64  `json:"timestamp"`
}

// ==========================================================
// WebSocket transport models
// ==========================================================

// WSMessage is the envelope for all WebSocket communication.
type WSMessage struct {
	Type    string      `json:"type"`    // "chat", "reaction", "system"
	Payload interface{} `json:"payload"`
}

// BroadcastMessage is the SNS/SQS envelope for cross-replica fan-out.
// SourceID is the sending hub's ID — the SQS consumer skips messages with a
// matching SourceID because those were already delivered directly to local clients.
type BroadcastMessage struct {
	Type     string    `json:"type"`               // "chat" or "reaction_update"
	RoomID   string    `json:"room_id"`
	Message  *Message  `json:"message,omitempty"`
	Reaction *Reaction `json:"reaction,omitempty"`
	SourceID string    `json:"source_id,omitempty"`
}

// ==========================================================
// Kafka event model — durable event stream for analytics
// ==========================================================

// KafkaEvent wraps any event for the Kafka topic.
// Consumers can filter by EventType to build different pipelines.
type KafkaEvent struct {
	EventType string      `json:"event_type"` // "message_sent", "reaction", "user_joined"
	RoomID    string      `json:"room_id"`
	Username  string      `json:"username"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// ==========================================================
// Common response types
// ==========================================================

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// ==========================================================
// Helpers
// ==========================================================

func NowMillis() int64 {
	return time.Now().UnixMilli()
}

func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
