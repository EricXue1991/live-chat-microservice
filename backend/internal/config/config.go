package config

import "os"

// Config holds all environment-driven configuration.
// Each field maps to an env var with a sensible default for local dev.
type Config struct {
	// --- Server ---
	Port string

	// --- AWS ---
	AWSRegion   string
	AWSEndpoint string // custom endpoint for LocalStack

	// --- PostgreSQL (users, rooms, relational data) ---
	PostgresDSN string

	// --- DynamoDB tables (messages, reactions — high-throughput) ---
	MessagesTable  string
	ReactionsTable string

	// --- Redis (cache + rate limiting + presence) ---
	RedisAddr     string
	RedisPassword string

	// --- Kafka (durable event stream + analytics) ---
	KafkaBrokers string
	KafkaTopic   string

	// --- S3 (file attachments) ---
	S3Bucket string

	// --- SNS (cross-replica broadcast fan-out) ---
	SNSTopicARN string

	// --- SQS ---
	ReactionQueueURL  string // reaction event queue (async aggregation)
	BroadcastQueueURL string // broadcast queue (SNS → SQS per replica)

	// --- JWT ---
	JWTSecret string

	// --- Feature flags ---
	ReactionMode string // "sync" or "async" (experiment 3 toggle)
	CacheEnabled bool   // toggle Redis cache (experiment 5)
	RateLimitRPS int    // requests per second per user, 0 = disabled
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8080"),
		AWSRegion:   getEnv("AWS_REGION", "us-east-1"),
		AWSEndpoint: getEnv("AWS_ENDPOINT_URL", ""),

		PostgresDSN: getEnv("POSTGRES_DSN", "postgres://livechat:livechat@localhost:5432/livechat?sslmode=disable"),

		MessagesTable:  getEnv("DYNAMODB_MESSAGES_TABLE", "livechat-messages"),
		ReactionsTable: getEnv("DYNAMODB_REACTIONS_TABLE", "livechat-reactions"),

		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),

		KafkaBrokers: getEnv("KAFKA_BROKERS", "localhost:9092"),
		KafkaTopic:   getEnv("KAFKA_TOPIC", "livechat-events"),

		S3Bucket:    getEnv("S3_BUCKET", "livechat-attachments"),
		SNSTopicARN: getEnv("SNS_TOPIC_ARN", ""),

		ReactionQueueURL:  getEnv("SQS_REACTION_QUEUE_URL", ""),
		BroadcastQueueURL: getEnv("SQS_BROADCAST_QUEUE_URL", ""),

		JWTSecret:    getEnv("JWT_SECRET", "dev-secret-change-in-prod"),
		ReactionMode: getEnv("REACTION_MODE", "async"),
		CacheEnabled: getEnv("CACHE_ENABLED", "true") == "true",
		RateLimitRPS: getEnvInt("RATE_LIMIT_RPS", 20),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n := 0
	for _, c := range v {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n == 0 {
		return fallback
	}
	return n
}
