#!/bin/bash

# WoT Relay Production Deployment Script
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
COMPOSE_FILE="docker-compose.prod.yml"
ENV_FILE=".env"
BACKUP_DIR="backups"
VERSION=${1:-"latest"}

echo -e "${GREEN}🚀 Starting WoT Relay Production Deployment${NC}"

# Check if .env file exists
if [ ! -f "$ENV_FILE" ]; then
    echo -e "${YELLOW}⚠️  .env file not found. Creating from example...${NC}"
    cp example.env "$ENV_FILE"
    echo -e "${RED}❌ Please edit $ENV_FILE with your configuration before running again.${NC}"
    exit 1
fi

# Validate required environment variables
echo -e "${YELLOW}🔍 Validating configuration...${NC}"
source "$ENV_FILE"

if [ -z "$RELAY_PUBKEY" ] || [ "$RELAY_PUBKEY" = "your_relay_pubkey_here" ]; then
    echo -e "${RED}❌ RELAY_PUBKEY is not set or is using default value${NC}"
    exit 1
fi

if [ -z "$RELAY_URL" ] || [ "$RELAY_URL" = "wss://your-relay-domain.com" ]; then
    echo -e "${RED}❌ RELAY_URL is not set or is using default value${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Configuration validation passed${NC}"

# Create backup directory
mkdir -p "$BACKUP_DIR"

# Backup existing data if it exists
if [ -d "data" ]; then
    echo -e "${YELLOW}📦 Creating backup of existing data...${NC}"
    BACKUP_NAME="backup_$(date +%Y%m%d_%H%M%S)"
    tar -czf "$BACKUP_DIR/$BACKUP_NAME.tar.gz" data/
    echo -e "${GREEN}✅ Backup created: $BACKUP_DIR/$BACKUP_NAME.tar.gz${NC}"
fi

# Pull latest images
echo -e "${YELLOW}📥 Pulling latest images...${NC}"
docker-compose -f "$COMPOSE_FILE" pull

# Build application
echo -e "${YELLOW}🔨 Building application...${NC}"
docker-compose -f "$COMPOSE_FILE" build --no-cache

# Stop existing containers
echo -e "${YELLOW}🛑 Stopping existing containers...${NC}"
docker-compose -f "$COMPOSE_FILE" down

# Start services
echo -e "${YELLOW}🚀 Starting services...${NC}"
docker-compose -f "$COMPOSE_FILE" up -d

# Wait for health check
echo -e "${YELLOW}⏳ Waiting for health check...${NC}"
timeout=60
counter=0
while [ $counter -lt $timeout ]; do
    if curl -f http://localhost:3334/health > /dev/null 2>&1; then
        echo -e "${GREEN}✅ Service is healthy${NC}"
        break
    fi
    echo -n "."
    sleep 1
    counter=$((counter + 1))
done

if [ $counter -eq $timeout ]; then
    echo -e "${RED}❌ Health check failed after ${timeout} seconds${NC}"
    echo -e "${YELLOW}📋 Checking logs...${NC}"
    docker-compose -f "$COMPOSE_FILE" logs --tail=50
    exit 1
fi

# Show status
echo -e "${GREEN}🎉 Deployment completed successfully!${NC}"
echo -e "${YELLOW}📊 Service Status:${NC}"
docker-compose -f "$COMPOSE_FILE" ps

echo -e "${YELLOW}📈 Health Check:${NC}"
curl -s http://localhost:3334/health | jq . 2>/dev/null || curl -s http://localhost:3334/health

echo -e "${YELLOW}📊 Stats:${NC}"
curl -s http://localhost:3334/stats | jq . 2>/dev/null || curl -s http://localhost:3334/stats

echo -e "${GREEN}🔗 Relay URL: $RELAY_URL${NC}"
echo -e "${GREEN}📊 Stats URL: http://localhost:3334/stats${NC}"
echo -e "${GREEN}❤️  Health URL: http://localhost:3334/health${NC}"

# Optional: Start with Nginx
if [ "$2" = "with-nginx" ]; then
    echo -e "${YELLOW}🌐 Starting with Nginx reverse proxy...${NC}"
    docker-compose -f "$COMPOSE_FILE" --profile with-nginx up -d nginx
    echo -e "${GREEN}🌐 Nginx: http://localhost:80 (redirects to HTTPS)${NC}"
    echo -e "${GREEN}🔒 HTTPS: https://localhost:443${NC}"
fi

echo -e "${GREEN}✅ Deployment script completed${NC}"
