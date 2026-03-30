package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"livechat/internal/analytics"
	"livechat/internal/auth"
	"livechat/internal/chat"
	"livechat/internal/config"
	"livechat/internal/media"
	"livechat/internal/middleware"
	"livechat/internal/reaction"
	"livechat/internal/ws"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"
	kafkaGo "github.com/segmentio/kafka-go"
)

func main() {
	// ========== 1. Load configuration ==========
	cfg := config.Load()
	log.Printf("[MAIN] starting LiveChat on port %s", cfg.Port)
	log.Printf("[MAIN] reaction_mode=%s cache=%v rate_limit=%d/s", cfg.ReactionMode, cfg.CacheEnabled, cfg.RateLimitRPS)

	// ========== 2. PostgreSQL (users, rooms) ==========
	pgDB, err := sql.Open("postgres", cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("[MAIN] PostgreSQL connect error: %v", err)
	}
	defer pgDB.Close()

	// Retry connection (wait for postgres container to be ready)
	for i := 0; i < 10; i++ {
		if err := pgDB.Ping(); err == nil {
			log.Println("[MAIN] PostgreSQL connected")
			break
		}
		log.Printf("[MAIN] waiting for PostgreSQL... (%d/10)", i+1)
		time.Sleep(2 * time.Second)
	}

	// ========== 3. Redis (cache, rate limiting, presence) ==========
	var rdb *redis.Client
	rdb = redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       0,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("[MAIN] Redis not available: %v (continuing without cache)", err)
		rdb = nil
	} else {
		log.Println("[MAIN] Redis connected")
	}

	// ========== 4. Kafka writer (event stream) ==========
	var kafkaWriter *kafkaGo.Writer
	kafkaWriter = &kafkaGo.Writer{
		Addr:         kafkaGo.TCP(cfg.KafkaBrokers),
		Topic:        cfg.KafkaTopic,
		Balancer:     &kafkaGo.LeastBytes{},
		BatchTimeout: 50 * time.Millisecond, // low latency batching
		Async:        true,                  // non-blocking writes
	}
	// Test Kafka connectivity
	conn, kafkaErr := kafkaGo.Dial("tcp", cfg.KafkaBrokers)
	if kafkaErr != nil {
		log.Printf("[MAIN] Kafka not available: %v (continuing without event stream)", kafkaErr)
		kafkaWriter = nil
	} else {
		conn.Close()
		log.Println("[MAIN] Kafka connected")
	}

	// ========== 5. AWS clients (DynamoDB, SNS, SQS, S3) ==========
	awsCfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion(cfg.AWSRegion),
	)
	if err != nil {
		log.Fatalf("[MAIN] AWS config error: %v", err)
	}

	// Custom endpoint for LocalStack
	if cfg.AWSEndpoint != "" {
		log.Printf("[MAIN] using custom AWS endpoint: %s", cfg.AWSEndpoint)
		resolver := aws.EndpointResolverWithOptionsFunc(
			func(service, region string, opts ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               cfg.AWSEndpoint,
					HostnameImmutable: true,
					PartitionID:       "aws",
					SigningRegion:     cfg.AWSRegion,
				}, nil
			},
		)
		awsCfg.EndpointResolverWithOptions = resolver
	}

	dbClient := dynamodb.NewFromConfig(awsCfg)
	snsClient := sns.NewFromConfig(awsCfg)
	sqsClient := sqs.NewFromConfig(awsCfg)
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.AWSEndpoint != "" {
			o.UsePathStyle = true // LocalStack requires path-style S3
		}
	})

	// ========== 6. Initialize handlers ==========
	authHandler := auth.NewHandler(pgDB, cfg)
	if err := authHandler.InitSchema(); err != nil {
		log.Printf("[MAIN] schema init warning: %v", err)
	}

	chatHandler := chat.NewHandler(dbClient, snsClient, rdb, kafkaWriter, cfg)
	reactionHandler := reaction.NewHandler(dbClient, sqsClient, rdb, kafkaWriter, cfg)
	mediaHandler := media.NewHandler(s3Client, cfg)

	// ========== 7. WebSocket hub ==========
	hub := ws.NewHub(cfg, sqsClient, rdb)
	go hub.Run()

	wsHandler := ws.NewWSHandler(hub, dbClient, snsClient, rdb, kafkaWriter, cfg)

	// ========== 8. Reaction aggregator (async mode) ==========
	if cfg.ReactionMode == "async" {
		aggregator := reaction.NewAggregator(dbClient, sqsClient, cfg)
		go aggregator.Start()
	}

	// ========== 9. Kafka analytics consumer ==========
	var analyticsConsumer *analytics.Consumer
	if kafkaWriter != nil {
		analyticsConsumer = analytics.NewConsumer(cfg)
		go analyticsConsumer.Start()
	}

	// ========== 10. Routes ==========
	router := mux.NewRouter()

	// Public endpoints (no auth)
	router.HandleFunc("/api/register", authHandler.Register).Methods("POST")
	router.HandleFunc("/api/login", authHandler.Login).Methods("POST")
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "timestamp": time.Now().UTC().Format(time.RFC3339)})
	}).Methods("GET")

	// System status (debugging)
	router.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]interface{}{
			"status":        "running",
			"reaction_mode": cfg.ReactionMode,
			"cache_enabled": cfg.CacheEnabled,
			"rate_limit":    cfg.RateLimitRPS,
			"rooms_online":  hub.GetAllRoomCounts(),
			"redis":         rdb != nil,
			"kafka":         kafkaWriter != nil,
		}
		if analyticsConsumer != nil {
			status["analytics"] = analyticsConsumer.GetStats()
		}
		// Experiment 3: async path queue depth (sync mode leaves these unset).
		if cfg.ReactionQueueURL != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			qout, err := sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl: aws.String(cfg.ReactionQueueURL),
				AttributeNames: []sqstypes.QueueAttributeName{
					sqstypes.QueueAttributeNameApproximateNumberOfMessages,
					sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
				},
			})
			cancel()
			if err == nil && qout.Attributes != nil {
				if v := qout.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)]; v != "" {
					if n, e := strconv.Atoi(v); e == nil {
						status["reaction_queue_visible"] = n
					}
				}
				if v := qout.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible)]; v != "" {
					if n, e := strconv.Atoi(v); e == nil {
						status["reaction_queue_inflight"] = n
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}).Methods("GET")

	// WebSocket (auth via query param)
	router.HandleFunc("/ws/rooms/{roomId}", wsHandler.HandleWebSocket)

	// Protected endpoints (JWT required)
	api := router.PathPrefix("/api").Subrouter()
	api.Use(middleware.JWTAuth(cfg))

	// Rate limiter middleware (after JWT so we know the username)
	rateLimiter := middleware.NewRateLimiter(rdb, cfg.RateLimitRPS, cfg.RateLimitRPS > 0)
	api.Use(rateLimiter.Middleware())

	api.HandleFunc("/messages", chatHandler.SendMessage).Methods("POST")
	api.HandleFunc("/messages", chatHandler.GetMessages).Methods("GET")
	api.HandleFunc("/reactions", reactionHandler.SubmitReaction).Methods("POST")
	api.HandleFunc("/reactions", reactionHandler.GetReactions).Methods("GET")
	api.HandleFunc("/upload", mediaHandler.Upload).Methods("POST")
	api.HandleFunc("/rooms", authHandler.GetRooms).Methods("GET")

	// Analytics endpoint
	if analyticsConsumer != nil {
		api.HandleFunc("/analytics", func(w http.ResponseWriter, r *http.Request) {
			roomID := r.URL.Query().Get("roomId")
			w.Header().Set("Content-Type", "application/json")
			if roomID != "" {
				json.NewEncoder(w).Encode(analyticsConsumer.GetRoomStats(roomID))
			} else {
				json.NewEncoder(w).Encode(analyticsConsumer.GetStats())
			}
		}).Methods("GET")
	}

	// ========== 11. CORS ==========
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	})

	// ========== 12. Start server ==========
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      corsHandler.Handler(router),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("[MAIN] shutting down...")
		ctx, cancel := context.WithTimeout(context.TODO(), 30*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		if analyticsConsumer != nil {
			analyticsConsumer.Stop()
		}
		if kafkaWriter != nil {
			kafkaWriter.Close()
		}
	}()

	log.Printf("[MAIN] server listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[MAIN] server error: %v", err)
	}
	log.Println("[MAIN] server stopped")
}
