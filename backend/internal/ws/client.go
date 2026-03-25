package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"livechat/internal/config"
	"livechat/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	kafkaGo "github.com/segmentio/kafka-go"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Client represents a single WebSocket connection.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	roomID   string
	username string
}

// WSHandler manages WebSocket upgrade and message processing.
type WSHandler struct {
	hub   *Hub
	db    *dynamodb.Client
	sns   *sns.Client
	rdb   *redis.Client
	kafka *kafkaGo.Writer
	cfg   *config.Config
}

func NewWSHandler(hub *Hub, db *dynamodb.Client, snsClient *sns.Client, rdb *redis.Client, kafka *kafkaGo.Writer, cfg *config.Config) *WSHandler {
	return &WSHandler{hub: hub, db: db, sns: snsClient, rdb: rdb, kafka: kafka, cfg: cfg}
}

// HandleWebSocket upgrades an HTTP request to a WebSocket connection.
// Auth is via query parameter because WebSocket handshake doesn't support custom headers.
// Route: WS /ws/rooms/{roomId}?token=xxx
func (h *WSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	roomID := vars["roomId"]
	if roomID == "" {
		http.Error(w, "roomId required", http.StatusBadRequest)
		return
	}

	// Validate JWT from query parameter
	tokenString := r.URL.Query().Get("token")
	if tokenString == "" {
		http.Error(w, "token required", http.StatusUnauthorized)
		return
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	claims, _ := token.Claims.(jwt.MapClaims)
	username, _ := claims["username"].(string)

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:      h.hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		roomID:   roomID,
		username: username,
	}
	h.hub.register <- client

	go h.readPump(client)
	go h.writePump(client)
}

// readPump reads messages from the client's WebSocket connection.
func (h *WSHandler) readPump(client *Client) {
	defer func() {
		h.hub.unregister <- client
		client.conn.Close()
	}()

	client.conn.SetReadLimit(maxMessageSize)
	client.conn.SetReadDeadline(time.Now().Add(pongWait))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, rawMsg, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[WS] read error: %v", err)
			}
			break
		}

		var wsMsg models.WSMessage
		if json.Unmarshal(rawMsg, &wsMsg) != nil {
			continue
		}

		if wsMsg.Type == "chat" {
			h.handleChatMessage(client, wsMsg.Payload)
		}
	}
}

// handleChatMessage processes a chat message received via WebSocket.
func (h *WSHandler) handleChatMessage(client *Client, payload interface{}) {
	payloadMap, ok := payload.(map[string]interface{})
	if !ok {
		return
	}
	content, _ := payloadMap["content"].(string)
	attachmentURL, _ := payloadMap["attachment_url"].(string)
	if content == "" {
		return
	}

	msgID := uuid.New().String()
	now := models.NowMillis()
	msg := models.Message{
		RoomID:        client.roomID,
		SortKey:       fmt.Sprintf("%d#%s", now, msgID),
		MessageID:     msgID,
		Username:      client.username,
		Content:       content,
		AttachmentURL: attachmentURL,
		Timestamp:     now,
		CreatedAt:     models.NowISO(),
	}

	// Write to DynamoDB
	item, _ := attributevalue.MarshalMap(msg)
	if _, err := h.db.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(h.cfg.MessagesTable),
		Item:      item,
	}); err != nil {
		log.Printf("[WS] DynamoDB PutItem error: %v", err)
		return
	}

	// Cache in Redis
	if h.cfg.CacheEnabled && h.rdb != nil {
		data, _ := json.Marshal(msg)
		key := "msgs:" + msg.RoomID
		ctx := context.Background()
		h.rdb.RPush(ctx, key, string(data))
		h.rdb.LTrim(ctx, key, -200, -1)
		h.rdb.Expire(ctx, key, 10*time.Minute)
	}

	// Broadcast to local clients
	h.hub.BroadcastToRoom(client.roomID, "chat", msg)

	// Broadcast to other replicas via SNS
	if h.cfg.SNSTopicARN != "" {
		broadcast := models.BroadcastMessage{Type: "chat", RoomID: client.roomID, Message: &msg}
		data, _ := json.Marshal(broadcast)
		h.sns.Publish(context.TODO(), &sns.PublishInput{
			TopicArn: aws.String(h.cfg.SNSTopicARN),
			Message:  aws.String(string(data)),
		})
	}

	// Publish to Kafka
	if h.kafka != nil {
		event := models.KafkaEvent{
			EventType: "message_sent",
			RoomID:    client.roomID,
			Username:  client.username,
			Timestamp: now,
			Data:      msg,
		}
		payload, _ := json.Marshal(event)
		h.kafka.WriteMessages(context.Background(), kafkaGo.Message{
			Key:   []byte(client.roomID),
			Value: payload,
		})
	}

	log.Printf("[WS] message via WebSocket: room=%s user=%s", client.roomID, client.username)
}

// writePump sends messages and pings to the client's WebSocket connection.
func (h *WSHandler) writePump(client *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				client.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
