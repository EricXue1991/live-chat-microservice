# LiveChat Infrastructure — Terraform (AWS Academy Learner Lab)

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ========== Variables ==========

variable "aws_region" {
  default = "us-east-1"
}

variable "project_name" {
  default = "livechat"
}

variable "replica_count" {
  default = 2
}

variable "jwt_secret" {
  default   = "change-this-in-production"
  sensitive = true
}

variable "reaction_mode" {
  default = "async"
}

variable "cache_enabled" {
  default = "true"
}

variable "rate_limit_rps" {
  default = "20"
}

variable "db_password" {
  default   = "livechat2024!"
  sensitive = true
}

# AWS Academy: iam:CreateRole is denied. ECS Fargate still needs a role whose trust policy includes
# ecs-tasks.amazonaws.com. LabRole often does NOT — then RegisterTaskDefinition returns "Role is not valid".
variable "lab_role_arn" {
  type        = string
  default     = ""
  description = "Fallback when ecs_fargate_role_arn is unset: default arn:aws:iam::<current account>:role/LabRole"

  validation {
    condition     = !can(regex("123456789012", var.lab_role_arn))
    error_message = "lab_role_arn must not use the example placeholder account 123456789012. Delete this line to auto-resolve LabRole, or use your 12-digit Account from: aws sts get-caller-identity"
  }
}

# Set this when LabRole is rejected (see terraform.tfvars.example). Must trust ecs-tasks.amazonaws.com.
variable "ecs_fargate_role_arn" {
  type        = string
  default     = ""
  description = "Override IAM ARN for both ECS execution + task role (use canonical ARN from IAM console)."

  validation {
    condition     = !can(regex("123456789012", var.ecs_fargate_role_arn))
    error_message = "ecs_fargate_role_arn must not use placeholder account 123456789012; use your real Account id."
  }
}

# Optional: use different roles if your lab requires it (default = same resolved LabRole for both).
variable "ecs_execution_role_arn" {
  type        = string
  default     = ""
  description = "Override execution role only (leave empty to use ecs_fargate_role_arn / LabRole)."

  validation {
    condition     = !can(regex("123456789012", var.ecs_execution_role_arn))
    error_message = "ecs_execution_role_arn must not use placeholder account 123456789012."
  }
}

variable "ecs_task_role_arn" {
  type        = string
  default     = ""
  description = "Override task role only (leave empty to use ecs_fargate_role_arn / LabRole)."

  validation {
    condition     = !can(regex("123456789012", var.ecs_task_role_arn))
    error_message = "ecs_task_role_arn must not use placeholder account 123456789012."
  }
}

data "aws_caller_identity" "current" {}

# Resolve LabRole ARN from IAM API (correct path/ARN format; avoids hand-typed ARN typos).
data "aws_iam_role" "lab" {
  name = "LabRole"
}

locals {
  lab_default_arn = trimspace(var.lab_role_arn) != "" ? trimspace(var.lab_role_arn) : data.aws_iam_role.lab.arn
  ecs_primary_arn = trimspace(var.ecs_fargate_role_arn) != "" ? trimspace(var.ecs_fargate_role_arn) : local.lab_default_arn
  ecs_exec_arn    = trimspace(var.ecs_execution_role_arn) != "" ? trimspace(var.ecs_execution_role_arn) : local.ecs_primary_arn
  ecs_task_arn    = trimspace(var.ecs_task_role_arn) != "" ? trimspace(var.ecs_task_role_arn) : local.ecs_primary_arn
}

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# ========== PostgreSQL (RDS) ==========

resource "aws_db_instance" "postgres" {
  identifier             = "${var.project_name}-pg"
  engine                 = "postgres"
  engine_version         = "16"
  instance_class         = "db.t3.micro"
  allocated_storage      = 20
  db_name                = "livechat"
  username               = "livechat"
  password               = var.db_password
  skip_final_snapshot    = true
  publicly_accessible    = true
  vpc_security_group_ids = [aws_security_group.db.id]
  tags = {
    Project = var.project_name
  }
}

resource "aws_security_group" "db" {
  name_prefix = "${var.project_name}-db-"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ========== Redis (ElastiCache) ==========

resource "aws_elasticache_cluster" "redis" {
  cluster_id         = "${var.project_name}-redis"
  engine             = "redis"
  node_type          = "cache.t3.micro"
  num_cache_nodes    = 1
  port               = 6379
  security_group_ids = [aws_security_group.redis.id]
  tags = {
    Project = var.project_name
  }
}

resource "aws_security_group" "redis" {
  name_prefix = "${var.project_name}-redis-"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 6379
    to_port     = 6379
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ========== DynamoDB ==========

resource "aws_dynamodb_table" "messages" {
  name         = "${var.project_name}-messages"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "room_id"
  range_key    = "sort_key"

  attribute {
    name = "room_id"
    type = "S"
  }

  attribute {
    name = "sort_key"
    type = "S"
  }

  tags = {
    Project = var.project_name
  }
}

resource "aws_dynamodb_table" "reactions" {
  name         = "${var.project_name}-reactions"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "room_id"
  range_key    = "reaction_type"

  attribute {
    name = "room_id"
    type = "S"
  }

  attribute {
    name = "reaction_type"
    type = "S"
  }

  tags = {
    Project = var.project_name
  }
}

# ========== S3 ==========

resource "aws_s3_bucket" "attachments" {
  bucket        = "${var.project_name}-attach-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
  tags = {
    Project = var.project_name
  }
}

resource "aws_s3_bucket_cors_configuration" "attachments" {
  bucket = aws_s3_bucket.attachments.id

  cors_rule {
    allowed_headers = ["*"]
    allowed_methods = ["GET", "PUT", "POST"]
    allowed_origins = ["*"]
    max_age_seconds = 3600
  }
}

# ========== SNS + SQS ==========

resource "aws_sns_topic" "broadcast" {
  name = "${var.project_name}-broadcast"
  tags = {
    Project = var.project_name
  }
}

resource "aws_sqs_queue" "reactions" {
  name                       = "${var.project_name}-reactions"
  visibility_timeout_seconds = 30
  receive_wait_time_seconds  = 20
  tags = {
    Project = var.project_name
  }
}

resource "aws_sqs_queue" "broadcast" {
  name                       = "${var.project_name}-broadcast"
  visibility_timeout_seconds = 10
  receive_wait_time_seconds  = 20
  tags = {
    Project = var.project_name
  }
}

resource "aws_sns_topic_subscription" "broadcast_sqs" {
  topic_arn = aws_sns_topic.broadcast.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.broadcast.arn
}

resource "aws_sqs_queue_policy" "broadcast" {
  queue_url = aws_sqs_queue.broadcast.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "sns.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.broadcast.arn
      Condition = {
        ArnEquals = {
          "aws:SourceArn" = aws_sns_topic.broadcast.arn
        }
      }
    }]
  })
}

# ========== Security Groups ==========

resource "aws_security_group" "api" {
  name_prefix = "${var.project_name}-api-"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "alb" {
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ========== ALB ==========

resource "aws_lb" "api" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = data.aws_subnets.default.ids
  tags = {
    Project = var.project_name
  }
}

resource "aws_lb_target_group" "api" {
  name        = "${var.project_name}-tg"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = data.aws_vpc.default.id
  target_type = "ip"

  health_check {
    path              = "/health"
    healthy_threshold = 2
    interval          = 30
  }
}

resource "aws_lb_listener" "api" {
  load_balancer_arn = aws_lb.api.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }
}

# ========== ECR ==========

resource "aws_ecr_repository" "api" {
  name         = "${var.project_name}-api"
  force_delete = true
}

# ========== ECS (uses LabRole) ==========

resource "aws_ecs_cluster" "main" {
  name = "${var.project_name}-cluster"
  tags = {
    Project = var.project_name
  }
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/ecs/${var.project_name}"
  retention_in_days = 7
}

resource "aws_ecs_task_definition" "api" {
  family                   = "${var.project_name}-api"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "512"
  memory                   = "1024"
  execution_role_arn       = local.ecs_exec_arn
  task_role_arn            = local.ecs_task_arn

  container_definitions = jsonencode([{
    name      = "${var.project_name}-api"
    image     = "${aws_ecr_repository.api.repository_url}:latest"
    essential = true

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    environment = [
      { name = "PORT", value = "8080" },
      { name = "AWS_REGION", value = var.aws_region },
      { name = "POSTGRES_DSN", value = "postgres://livechat:${var.db_password}@${aws_db_instance.postgres.endpoint}/livechat?sslmode=require" },
      { name = "REDIS_ADDR", value = "${aws_elasticache_cluster.redis.cache_nodes[0].address}:6379" },
      { name = "KAFKA_BROKERS", value = "" },
      { name = "DYNAMODB_MESSAGES_TABLE", value = aws_dynamodb_table.messages.name },
      { name = "DYNAMODB_REACTIONS_TABLE", value = aws_dynamodb_table.reactions.name },
      { name = "S3_BUCKET", value = aws_s3_bucket.attachments.id },
      { name = "SNS_TOPIC_ARN", value = aws_sns_topic.broadcast.arn },
      { name = "SQS_REACTION_QUEUE_URL", value = aws_sqs_queue.reactions.url },
      { name = "SQS_BROADCAST_QUEUE_URL", value = aws_sqs_queue.broadcast.url },
      { name = "JWT_SECRET", value = var.jwt_secret },
      { name = "REACTION_MODE", value = var.reaction_mode },
      { name = "CACHE_ENABLED", value = var.cache_enabled },
      { name = "RATE_LIMIT_RPS", value = var.rate_limit_rps },
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.api.name
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "api"
      }
    }
  }])
}

resource "aws_ecs_service" "api" {
  name            = "${var.project_name}-api"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.api.arn
  desired_count   = var.replica_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = data.aws_subnets.default.ids
    security_groups  = [aws_security_group.api.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "${var.project_name}-api"
    container_port   = 8080
  }

  depends_on = [aws_lb_listener.api]
}

# ========== Outputs ==========

output "alb_dns" {
  value = aws_lb.api.dns_name
}

output "ecr_repo" {
  value = aws_ecr_repository.api.repository_url
}

output "postgres_host" {
  value = aws_db_instance.postgres.endpoint
}

output "redis_host" {
  value = aws_elasticache_cluster.redis.cache_nodes[0].address
}

output "dynamodb" {
  value = {
    messages  = aws_dynamodb_table.messages.name
    reactions = aws_dynamodb_table.reactions.name
  }
}

output "sns_topic" {
  value = aws_sns_topic.broadcast.arn
}

output "sqs_queues" {
  value = {
    reactions = aws_sqs_queue.reactions.url
    broadcast = aws_sqs_queue.broadcast.url
  }
}

# Debug: ARNs used in aws_ecs_task_definition (verify in IAM if RegisterTaskDefinition says "Role is not valid").
output "ecs_roles_resolved" {
  value = {
    execution_role_arn = local.ecs_exec_arn
    task_role_arn      = local.ecs_task_arn
  }
}
