#!/bin/bash
# Build, push to ECR, and update ECS service.
set -e

AWS_REGION="${AWS_REGION:-us-east-1}"
PROJECT="livechat"
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
ECR="${ACCOUNT}.dkr.ecr.${AWS_REGION}.amazonaws.com/${PROJECT}-api"

echo "=== LiveChat Deploy ==="
echo "Account: ${ACCOUNT} | Region: ${AWS_REGION}"

echo "[1/4] ECR login..."
aws ecr get-login-password --region ${AWS_REGION} | \
  docker login --username AWS --password-stdin ${ACCOUNT}.dkr.ecr.${AWS_REGION}.amazonaws.com

# Fargate runs linux/amd64. Builds on Apple Silicon default to arm64 and will fail at pull with:
# "Manifest does not contain descriptor matching platform 'linux/amd64'"
echo "[2/4] Building for linux/amd64 (Fargate)..."
cd backend && docker build --platform linux/amd64 -t ${PROJECT}-api .

echo "[3/4] Pushing..."
docker tag ${PROJECT}-api:latest ${ECR}:latest
docker push ${ECR}:latest

echo "[4/4] Updating ECS..."
aws ecs update-service --cluster ${PROJECT}-cluster --service ${PROJECT}-api \
  --force-new-deployment --region ${AWS_REGION}

echo "=== Deploy initiated! ==="
