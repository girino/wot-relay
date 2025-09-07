# WoT Relay - Production Deployment Guide

This guide covers deploying the WoT Relay in a production environment with Docker Compose, health checks, monitoring, and SSL termination.

## 🚀 Quick Start

1. **Clone and configure:**
   ```bash
   git clone <repository-url>
   cd wot-relay
   cp env.example .env
   # Edit .env with your configuration
   ```

2. **Deploy:**
   ```bash
   ./deploy.sh
   ```

3. **Deploy with monitoring:**
   ```bash
   ./deploy.sh latest with-monitoring
   ```

## 📋 Prerequisites

- Docker and Docker Compose
- SSL certificates (for HTTPS)
- Domain name pointing to your server
- At least 2GB RAM and 1 CPU core

## ⚙️ Configuration

### Required Environment Variables

Edit `.env` file with your configuration:

```bash
# Relay Configuration (REQUIRED)
RELAY_NAME=Your Relay Name
RELAY_PUBKEY=your_64_character_hex_pubkey
RELAY_DESCRIPTION=Your relay description
RELAY_URL=wss://your-domain.com
RELAY_CONTACT=your_contact_pubkey

# Database Configuration
DB_PATH=/app/data/relay.db
INDEX_PATH=/app/templates/index.html
STATIC_PATH=/app/templates/static

# Performance Configuration
REFRESH_INTERVAL_HOURS=3
MINIMUM_FOLLOWERS=1
WOT_DEPTH=2
MAX_AGE_DAYS=0

# Archiving Configuration
ARCHIVAL_SYNC=TRUE
ARCHIVE_REACTIONS=FALSE
ARCHIVE_MAX_DAYS=15
PROFILE_MAX_AGE_DAYS=30

# Logging Configuration
LOG_LEVEL=INFO
```

### Optional Configuration

- `IGNORE_FOLLOWS_LIST`: Comma-separated list of pubkeys to ignore
- `MAX_TRUST_NETWORK`: Maximum trust network size (default: 40000)
- `MAX_RELAYS`: Maximum number of relays (default: 1000)
- `MAX_ONE_HOP_NETWORK`: Maximum one-hop network size (default: 50000)

## 🐳 Docker Services

### Core Services

- **wot-relay**: Main relay application (includes eventstore patch for QueryKindsLimit)
- **nginx**: Reverse proxy with SSL termination (optional)

### Monitoring Services (with-monitoring profile)

- **prometheus**: Metrics collection
- **grafana**: Metrics visualization

## 🔧 Deployment Options

### Important: Eventstore Patch

The Docker build automatically applies a patch to the `eventstore` library to fix the `QueryKindsLimit` issue. This patch:

- Increases the default limit from 10 to 50 kinds per query
- Uses the configured `QueryKindsLimit` value from the database configuration
- Prevents "too many kinds" errors during WoT building and archiving

The patch is applied automatically during the Docker build process.

### Basic Deployment
```bash
docker-compose -f docker-compose.prod.yml up -d
```

### With Nginx (SSL termination)
```bash
docker-compose -f docker-compose.prod.yml --profile with-nginx up -d
```

### With Full Monitoring Stack
```bash
docker-compose -f docker-compose.prod.yml --profile with-monitoring up -d
```

## 📊 Monitoring & Health Checks

### Health Endpoints

- **Health Check**: `http://localhost:3334/health`
- **Stats**: `http://localhost:3334/stats`

### Health Check Response

```json
{
  "status": "healthy",
  "timestamp": 1691234567,
  "uptime": 3600.5
}
```

### Stats Response

```json
{
  "relay": {
    "name": "WoT Relay",
    "pubkey": "abc123...",
    "description": "Web of Trust Relay",
    "version": "1.1.0"
  },
  "uptime": {
    "start_time": "2023-08-04T10:00:00Z",
    "duration": 3600.5
  },
  "network": {
    "size": 15000,
    "last_wot_refresh": "2023-08-04T11:00:00Z",
    "last_profile_refresh": "2023-08-04T11:05:00Z",
    "last_archiving": "2023-08-04T11:10:00Z"
  },
  "events": {
    "total": 50000,
    "trusted": 45000,
    "untrusted": 5000,
    "processing_queue": 0
  },
  "system": {
    "goroutines": 150,
    "memory_mb": 256,
    "gc_runs": 10,
    "active_connections": 25
  },
  "errors": {
    "count": 0,
    "last_error": null,
    "last_error_msg": ""
  }
}
```

## 🔒 SSL Configuration

1. **Obtain SSL certificates** and place them in `./ssl/`:
   - `cert.pem` - Certificate file
   - `key.pem` - Private key file

2. **Update nginx.conf** with your domain name

3. **Deploy with nginx:**
   ```bash
   docker-compose -f docker-compose.prod.yml --profile with-nginx up -d
   ```

## 📈 Monitoring Setup

### Prometheus
- URL: `http://localhost:9090`
- Scrapes metrics from `/stats` endpoint every 30 seconds

### Grafana
- URL: `http://localhost:3000`
- Default credentials: `admin/admin`
- Import dashboards from `./grafana/dashboards/`

## 🔄 Maintenance

### View Logs
```bash
# All services
docker-compose -f docker-compose.prod.yml logs -f

# Specific service
docker-compose -f docker-compose.prod.yml logs -f wot-relay
```

### Restart Services
```bash
docker-compose -f docker-compose.prod.yml restart
```

### Update Application
```bash
./deploy.sh
```

### Backup Data
```bash
# Data is automatically backed up before deployment
# Manual backup:
docker-compose -f docker-compose.prod.yml exec wot-relay tar -czf /tmp/backup.tar.gz /app/data
docker cp $(docker-compose -f docker-compose.prod.yml ps -q wot-relay):/tmp/backup.tar.gz ./backup-$(date +%Y%m%d).tar.gz
```

### Restore Data
```bash
# Stop services
docker-compose -f docker-compose.prod.yml down

# Restore data
tar -xzf backup-YYYYMMDD.tar.gz
mv data ./data

# Start services
docker-compose -f docker-compose.prod.yml up -d
```

## 🚨 Troubleshooting

### Health Check Failing
1. Check logs: `docker-compose -f docker-compose.prod.yml logs wot-relay`
2. Verify configuration in `.env`
3. Check resource usage: `docker stats`

### High Memory Usage
1. Reduce `MAX_TRUST_NETWORK` in `.env`
2. Increase `REFRESH_INTERVAL_HOURS`
3. Check for memory leaks in logs

### Slow Performance
1. Increase `WOT_DEPTH` gradually
2. Check database size and optimize
3. Monitor network connectivity to relays

### Database Issues
1. Check disk space: `df -h`
2. Verify database permissions
3. Consider database optimization

## 📚 API Endpoints

- `GET /` - Relay information page
- `GET /health` - Health check
- `GET /stats` - Detailed metrics
- `WS /` - Nostr WebSocket endpoint

## 🔐 Security Considerations

1. **Firewall**: Only expose ports 80, 443, and 3334
2. **SSL**: Always use HTTPS in production
3. **Updates**: Regularly update Docker images
4. **Monitoring**: Set up alerts for health check failures
5. **Backups**: Regular automated backups
6. **Access Control**: Restrict access to monitoring endpoints

## 📞 Support

For issues and questions:
1. Check the logs first
2. Review this documentation
3. Check the health and stats endpoints
4. Create an issue with logs and configuration details
