module livechat

go 1.21

require (
	github.com/aws/aws-sdk-go-v2 v1.30.3
	github.com/aws/aws-sdk-go-v2/config v1.27.27
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.14.10
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.34.4
	github.com/aws/aws-sdk-go-v2/service/s3 v1.58.3
	github.com/aws/aws-sdk-go-v2/service/sns v1.31.3
	github.com/aws/aws-sdk-go-v2/service/sqs v1.34.3
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/lib/pq v1.10.9
	github.com/redis/go-redis/v9 v9.5.3
	github.com/rs/cors v1.11.0
	github.com/segmentio/kafka-go v0.4.47
	golang.org/x/crypto v0.25.0
)
