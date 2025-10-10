# WoT Relay

WoT Relay is a high-performance Nostr relay that saves all the notes from people you follow and their extended network (Web of Trust). Built on the [Khatru](https://khatru.nostr.technology) framework with production-ready features including health monitoring, structured logging, and anonymous access via Tor.

## Features

- **Web of Trust**: Automatically discovers and archives notes from your extended network
- **Performance Optimized**: Multi-threaded processing, batch operations, and memory monitoring
- **Production Ready**: Health checks, structured logging, metrics, and graceful shutdown
- **Anonymous Access**: Built-in Tor proxy for privacy-preserving access
- **Docker Support**: Complete containerization with production deployment
- **Auto-healing**: Automatic container health monitoring and restart
- **Configurable**: Extensive environment variable configuration

## Available Relays

Don't want to run the relay, just want to connect to some? Here are some available relays:

- [wss://wot.utxo.one](https://wot.utxo.one)
- [wss://nostrelites.org](https://nostrelites.org)
- [wss://wot.nostr.party](https://wot.nostr.party)
- [wss://wot.sovbit.host](https://wot.sovbit.host)
- [wss://wot.girino.org](https://wot.girino.org)
- [wss://relay.lnau.net](https://relay.lnau.net)
- [wss://wot.siamstr.com](https://wot.siamstr.com)
- [wss://relay.lexingtonbitcoin.org](https://relay.lexingtonbitcoin.org)
- [wss://wot.azzamo.net](https://wot.azzamo.net)
- [wss://wot.swarmstr.com](https://wot.swarmstr.com)
- [wss://zap.watch](https://zap.watch)
- [wss://satsage.xyz](https://satsage.xyz)
- [wss://wons.calva.dev](https://wons.calva.dev)
- [wss://wot.zacoos.com](https://wot.zacoos.com)
- [wss://wot.shaving.kiwi](https://wot.shaving.kiwi)
- [wss://wot.tealeaf.dev](https://wot.tealeaf.dev)
- [wss://wot.nostr.net](https://wot.nostr.net)
- [wss://relay.goodmorningbitcoin.com](https://relay.goodmorningbitcoin.com)
- [wss://wot.sudocarlos.com](wss://wot.sudocarlos.com)

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
  - [Docker (Recommended)](#docker-recommended)
  - [Manual Installation](#manual-installation)
- [Configuration](#configuration)
  - [Required Variables](#required-variables)
  - [Performance Configuration](#performance-configuration)
  - [Archiving Configuration](#archiving-configuration)
  - [Query Limits](#query-limits)
  - [Optional Filters](#optional-filters)
  - [Profiling Configuration](#profiling-configuration)
- [Running the Relay](#running-the-relay)
  - [Production Deployment](#production-deployment)
  - [Systemd Service](#systemd-service)
  - [Nginx Reverse Proxy](#nginx-reverse-proxy)
  - [SSL with Certbot](#ssl-with-certbot)
  - [Tor Hidden Service](#tor-hidden-service)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)
- [License](#license)

## Prerequisites

### For Docker Deployment
- Docker and Docker Compose
- At least 2GB RAM and 1 CPU core
- Domain name pointing to your server (for SSL)

### For Manual Installation
- **Go 1.21+**: Download from [golang.org](https://golang.org/dl/)
- **Build Essentials**: On Linux, install with `sudo apt install build-essential`

## Quick Start

### Docker (Recommended)

#### Development/Testing
```bash
# Clone the repository
git clone https://github.com/bitvora/wot-relay.git
cd wot-relay

# Copy environment template
cp example.env .env

# Edit .env with your configuration
nano .env

# Start with Docker Compose
docker compose up --build -d
```

#### Production Deployment
```bash
# Production deployment (includes Tor proxy)
docker-compose -f docker-compose.prod.yml up -d

# With Nginx reverse proxy
docker-compose -f docker-compose.prod.yml --profile with-nginx up -d
```

**Production Features:**
- Multi-stage optimized Docker build
- Auto-healing container monitoring
- Anonymous access via Tor proxy
- Health checks and structured logging
- Resource limits and restart policies

#### Tor Hidden Service
```bash
# Start with Tor hidden service
docker compose -f docker-compose.tor.yml up --build -d

# Find your onion address
cat tor/data/relay/hostname
```

### Manual Installation

#### 1. Clone the Repository
```bash
git clone https://github.com/bitvora/wot-relay.git
cd wot-relay
```

#### 2. Configure Environment
```bash
# Copy the example environment file
cp example.env .env

# Edit with your settings
nano .env
```

See [Configuration](#configuration) section for details.

#### 3. Build the Relay
```bash
# Build with version information
go build -ldflags "-X main.version=$(git describe --tags --always)"
```

#### 4. Run the Relay
```bash
./wot-relay
```

The relay will be available at `http://localhost:3334`

## Configuration

All configuration is done via environment variables in the `.env` file.

### Required Variables

```bash
# Relay Information (REQUIRED)
RELAY_NAME="YourRelayName"
RELAY_PUBKEY="your_64_character_hex_pubkey"  # Convert npub to hex at https://nostrcheck.me/converter/
RELAY_DESCRIPTION="Your relay description"
RELAY_URL="wss://your-domain.com"
RELAY_CONTACT="your_contact_pubkey"

# Paths
DB_PATH="/app/db/relay.db"                    # Database location
INDEX_PATH="/app/templates/index.html"         # Web interface
STATIC_PATH="/app/templates/static"            # Static assets
```

### Performance Configuration

```bash
# Web of Trust Settings
REFRESH_INTERVAL_HOURS=3                       # How often to refresh WoT (default: 3 hours)
MINIMUM_FOLLOWERS=1                            # Minimum followers to be included in WoT
WOT_DEPTH=2                                    # Network depth (1 or 2, default: 2)

# Worker Configuration
# WORKER_COUNT=auto                            # Default: 2 per CPU core (auto-detected)
WORKER_MULTIPLIER=50                           # Multiplier for worker count

# Network Configuration
SEED_RELAYS=wss://relay.primal.net,wss://relay.damus.io,wss://nos.lol
IGNORE_FOLLOWS_LIST=                           # Comma-separated pubkeys to ignore
```

### Archiving Configuration

```bash
# Archiving Settings
ARCHIVAL_SYNC=TRUE                             # Archive notes from WoT (recommended: TRUE)
ARCHIVE_REACTIONS=FALSE                        # Archive reactions (not recommended unless needed)
ARCHIVE_MAX_DAYS=15                            # Maximum age of notes to archive (0 = all)
MAX_AGE_DAYS=0                                 # Maximum age for stored events (0 = keep all)
PROFILE_MAX_AGE_DAYS=30                        # How old profiles can be before refresh

# Logging
LOG_LEVEL=INFO                                 # DEBUG, INFO, WARN, ERROR
```

### Query Limits

Control the maximum complexity of queries allowed (all optional, default: 50):

```bash
# Database Query Limits
QUERY_IDS_LIMIT=50                             # Max event IDs in a filter
QUERY_AUTHORS_LIMIT=50                         # Max authors (pubkeys) in a filter
QUERY_KINDS_LIMIT=50                           # Max event kinds in a filter
QUERY_TAGS_LIMIT=50                            # Max total tag values across all tags
```

**Recommendations:**
- **Default (50)**: Good for most relays
- **High-performance**: 100/100/50/100
- **Resource-constrained**: 25/25/25/25
- **Open data**: 200/200/50/100

### Optional Filters

#### Anti-Sync Bots Filter

Detects and blocks automated scrapers (disabled by default):

```bash
ENABLE_ANTI_SYNC_BOTS_FILTER=FALSE             # TRUE to enable (requires auth + WoT for bulk queries)
```

**When enabled:**
- Large queries require authentication
- Only WoT members can run bulk queries
- Helps prevent data scraping

#### Kind 1984 Filter

Restrict access to moderation/report events (disabled by default):

```bash
ENABLE_KIND1984_FILTER=FALSE                   # TRUE to enable (requires auth + WoT)
```

**When enabled:**
- Kind 1984 queries require authentication
- Only WoT members can query reports
- Blocks expensive queries with both authors and tags

### Profiling Configuration

Performance monitoring layer (disabled by default for maximum speed):

```bash
ENABLE_PROFILING=FALSE                         # TRUE to enable detailed performance metrics
```

**When enabled:**
- Tracks operation statistics (calls, duration, averages)
- Semaphore-based concurrency control
- Slow query detection and warnings
- Exposed via `/stats` endpoint

**When disabled (default):**
- Maximum performance (no profiling overhead)
- Direct database access
- Lower memory usage
- Still provides connection stats

**Recommendation:** Enable during initial deployment for monitoring, then disable for production.

### Autoheal Configuration (Docker only)

```bash
AUTOHEAL_INTERVAL=5                            # Health check interval (seconds)
AUTOHEAL_START_PERIOD=0                        # Grace period before checks start
AUTOHEAL_DEFAULT_STOP_TIMEOUT=10               # Container stop timeout
```

## Running the Relay

### Production Deployment

For production environments, use the optimized Docker setup:

```bash
# Standard production (includes Tor)
docker-compose -f docker-compose.prod.yml up -d

# With Nginx SSL termination
docker-compose -f docker-compose.prod.yml --profile with-nginx up -d

# View logs
docker-compose -f docker-compose.prod.yml logs -f

# Restart services
docker-compose -f docker-compose.prod.yml restart

# Stop services
docker-compose -f docker-compose.prod.yml down
```

### Systemd Service

To run the relay as a system service:

#### 1. Create Service File
```bash
sudo nano /etc/systemd/system/wot-relay.service
```

#### 2. Add Configuration
```ini
[Unit]
Description=WoT Relay Service
After=network.target

[Service]
ExecStart=/home/ubuntu/wot-relay/wot-relay
WorkingDirectory=/home/ubuntu/wot-relay
Restart=always
MemoryLimit=2G

[Install]
WantedBy=multi-user.target
```

#### 3. Enable and Start
```bash
# Reload systemd
sudo systemctl daemon-reload

# Start the service
sudo systemctl start wot-relay

# Enable on boot
sudo systemctl enable wot-relay

# Check status
sudo systemctl status wot-relay

# View logs
sudo journalctl -u wot-relay -f
```

#### Permission Issues

If you encounter database permission issues:
```bash
sudo chmod -R 777 /path/to/db
```

### Nginx Reverse Proxy

Serve the relay over HTTP/HTTPS with Nginx:

#### 1. Configure Nginx
```bash
sudo nano /etc/nginx/sites-available/wot-relay
```

#### 2. Add Configuration
```nginx
server {
    listen 80;
    server_name yourdomain.com;

    location / {
        proxy_pass http://localhost:3334;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

#### 3. Enable Site
```bash
# Create symbolic link
sudo ln -s /etc/nginx/sites-available/wot-relay /etc/nginx/sites-enabled/

# Test configuration
sudo nginx -t

# Restart Nginx
sudo systemctl restart nginx
```

### SSL with Certbot

Add HTTPS support with Let's Encrypt:

```bash
# Install Certbot
sudo apt-get update
sudo apt-get install certbot python3-certbot-nginx

# Generate certificate
sudo certbot --nginx -d yourdomain.com

# Test automatic renewal
sudo certbot renew --dry-run
```

Certbot will automatically configure Nginx for HTTPS.

### Tor Hidden Service

Run as a Tor hidden service for anonymous access:

```bash
# Start with Docker
docker compose -f docker-compose.tor.yml up --build -d

# Find your onion address
cat tor/data/relay/hostname
```

Your relay will be accessible at `http://[your-onion-address].onion`

## Monitoring

### Health Check Endpoint

```bash
curl http://localhost:3334/health
```

Response:
```json
{
  "status": "healthy",
  "timestamp": 1691234567,
  "uptime": 3600.5
}
```

### Stats Endpoint

```bash
curl http://localhost:3334/stats
```

Response includes:
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
  "database": {
    "profiling_enabled": false,
    "connections": {
      "open_connections": 5,
      "in_use": 2,
      "idle": 3
    }
  },
  "errors": {
    "count": 0,
    "last_error": null,
    "last_error_msg": ""
  }
}
```

### Logging

Logs are structured JSON with configurable levels (DEBUG, INFO, WARN, ERROR):

```bash
# View logs (systemd)
sudo journalctl -u wot-relay -f

# View logs (Docker)
docker-compose -f docker-compose.prod.yml logs -f wot-relay

# View logs (manual)
./wot-relay | tee relay.log
```

## Troubleshooting

### Health Check Failing
1. Check logs for errors
2. Verify `.env` configuration
3. Check resource usage: `docker stats`
4. Ensure database path is writable

### High Memory Usage
1. Increase `REFRESH_INTERVAL_HOURS`
2. Reduce `WOT_DEPTH` (try 1 instead of 2)
3. Set `ARCHIVE_REACTIONS=FALSE`
4. Check for memory leaks in logs

### Slow Performance
1. Check `/stats` endpoint for metrics
2. Monitor query times (enable profiling temporarily)
3. Reduce `QUERY_AUTHORS_LIMIT` and `QUERY_TAGS_LIMIT`
4. Check network connectivity to seed relays
5. Optimize database: `PRAGMA optimize;`

### Database Issues
1. Check disk space: `df -h`
2. Verify database permissions
3. Check database integrity: `sqlite3 db/relay.db "PRAGMA integrity_check;"`
4. Consider running `VACUUM` to reclaim space

### Build Errors
```bash
# Clean build
rm -rf wot-relay
go clean -cache
go build -ldflags "-X main.version=$(git describe --tags --always)"
```

### Connection Issues
1. Verify port 3334 is accessible
2. Check firewall rules: `sudo ufw status`
3. Test WebSocket connection: `wscat -c ws://localhost:3334`
4. Review Nginx logs if using reverse proxy

## Architecture

### Components

- **Main Relay**: Nostr protocol implementation using Khatru framework
- **Database**: SQLite with custom indexes and optimizations (see `pkg/newsqlite3/`)
- **Profiling Layer**: Optional performance monitoring (see `pkg/profiling/`)
- **Logger**: Structured logging system (see `pkg/logger/`)
- **Worker Pool**: Multi-threaded event processing
- **Health Monitor**: Container orchestration support

### Database Optimizations

- **WAL Mode**: Better concurrency
- **Custom Indexes**: Optimized for WoT queries
- **Query Limits**: Prevent expensive queries
- **Connection Pooling**: Efficient connection management
- **Periodic Maintenance**: Automatic ANALYZE and optimization

### Package Documentation

For detailed documentation on internal packages:
- [pkg/logger/](pkg/logger/README.md) - Structured logging
- [pkg/newsqlite3/](pkg/newsqlite3/README.md) - SQLite backend wrapper
- [pkg/profiling/](pkg/profiling/README.md) - Performance profiling

## API Endpoints

- `GET /` - Relay information page
- `GET /health` - Health check endpoint
- `GET /stats` - Detailed metrics and statistics
- `WS /` - Nostr WebSocket endpoint (NIP-01)

## Security Considerations

1. **Firewall**: Only expose ports 80 and 443; do not expose port 3334 to the public internet
2. **SSL**: Always use HTTPS in production
3. **Updates**: Regularly update Docker images and dependencies
4. **Monitoring**: Set up alerts for health check failures
5. **Backups**: Regular automated database backups
6. **Access Control**: Restrict monitoring endpoints if needed
7. **Rate Limiting**: Built-in rate limiters protect against abuse

## Maintenance

### Backup Database
```bash
# Docker
docker-compose -f docker-compose.prod.yml exec wot-relay tar -czf /tmp/backup.tar.gz /app/data
docker cp $(docker-compose -f docker-compose.prod.yml ps -q wot-relay):/tmp/backup.tar.gz ./backup-$(date +%Y%m%d).tar.gz

# Manual
tar -czf backup-$(date +%Y%m%d).tar.gz db/
```

### Restore Database
```bash
# Stop services
docker-compose -f docker-compose.prod.yml down

# Restore data
tar -xzf backup-YYYYMMDD.tar.gz
mv data ./data

# Start services
docker-compose -f docker-compose.prod.yml up -d
```

### Update Deployment
```bash
# Use automated deployment script
./deploy.sh

# Or manually
git pull
docker-compose -f docker-compose.prod.yml down
docker-compose -f docker-compose.prod.yml up -d --build
```

### Database Optimization
```bash
# Connect to database
sqlite3 db/relay.db

# Run optimization
PRAGMA optimize;
ANALYZE;

# Check integrity
PRAGMA integrity_check;

# Reclaim space (may take time)
VACUUM;
```

## License

This project is licensed under the MIT License.

## Support

- **Issues**: [GitHub Issues](https://github.com/bitvora/wot-relay/issues)
- **Documentation**: Check this README and package docs
- **Stats**: Monitor `/health` and `/stats` endpoints
- **Logs**: Check structured logs for detailed information
