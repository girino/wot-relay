# Release Notes

## v1.5.0 (2025-10-09)

### 🎉 Overview

Major refactoring release focused on documentation, performance, and code organization. Complete documentation overhaul with 2,170+ lines of new content, significant query performance improvements (10-20x faster for kind 1984), and cleaner package architecture.

### ⚠️ Breaking Changes

- **Package Renamed**: `pkg/newsqlite3` → `pkg/sqlite3`
- **Removed Unused Variables**: `MAX_TRUST_NETWORK`, `MAX_ONE_HOP_NETWORK`, `MAX_RELAYS`

### ✨ Major Features

- **📚 Documentation Overhaul**: Unified README (654 lines) + comprehensive package docs (1,516 lines)
- **🚀 Query Performance**: Migration 9 with specialized indexes for kind 1984 (10-20x faster)
- **⚙️ Configurable Limits**: `QUERY_IDS_LIMIT`, `QUERY_AUTHORS_LIMIT`, `QUERY_KINDS_LIMIT`, `QUERY_TAGS_LIMIT`
- **🎛️ Optional Filters**: Anti-sync bots, Kind 1984 filtering, and profiling (all opt-in)

### 🐛 Bug Fixes

- Fixed duplicate timestamps in logger output
- Optimized slow kind 1984 queries (200-1200ms → 10-100ms)
- Improved query plan selection with index hints

### 🔄 Refactoring

- Renamed `pkg/newsqlite3` to `pkg/sqlite3`
- Removed eventstore patches (no longer needed)
- Deleted 9 redundant documentation files
- Cleaned up Go dependencies
- Removed unused `pkg/sqlite/` package

### 📈 Performance Improvements

- Increased read semaphore from 6 to 32 (5x concurrent reads)
- Doubled cache size (64MB → 128MB)
- Doubled mmap size (256MB → 512MB)
- Removed tag_data column (reduced database size)
- Added covering and partial indexes

### 📦 Documentation

- **Added**: `pkg/logger/README.md` (387 lines)
- **Added**: `pkg/sqlite3/README.md` (536 lines)
- **Added**: `pkg/profiling/README.md` (593 lines)
- **Deleted**: 9 redundant config/analysis docs
- **Updated**: Main README with complete guide

**Full Details**: See [RELEASE_NOTES_v1.5.0.md](RELEASE_NOTES_v1.5.0.md)

---

## v1.4.0 (2025-XX-XX)

### 🔧 Major Changes

- **Structured Logger Package**: Normalized all logging to use structured logger package
- **Improved Error Logging**: Enhanced error logging in QueryEvents with context cancellation capture
- **Attribution Update**: Updated footer to reflect girino as builder, loosely based on Bitvora

### 🐛 Bug Fixes

- Better context error handling in query events
- Improved error context capture and logging

### 📝 Logging Improvements

- Consistent structured logging across all components
- Better error context and debugging information
- Normalized log format throughout codebase

---

## v1.3.0 (2025-XX-XX)

### 🚀 Major Features

- **Package Reorganization**: Moved packages from `internal/` to `pkg/` for external importability
- **Profiling Package**: Separated profiling into distinct package with performance monitoring
- **SQLite Optimizations**: Dedicated package for SQLite performance optimizations
- **Semaphore-Based Concurrency**: Hybrid eventstore with concurrent reads and serialized writes

### 🔧 Performance Improvements

- **Comprehensive Database Indexes**: Added optimized indexes for common query patterns
- **Dual Timing Metrics**: Track both total time and pure database time
- **Automatic Stats Cleanup**: Clean up stats at end of refresh cycles
- **On-Demand Relay Connections**: Better connection management
- **Worker Pool Enhancements**: 
  - Default to 2 workers per CPU core
  - Configurable worker count and multiplier
  - Added worker statistics to stats endpoint

### 🐛 Bug Fixes

- Fixed context cancellation errors and improved performance
- Fixed filter validation to clean empty tag arrays instead of rejecting
- Fixed type assertion for RelayWrapper in stats endpoint
- Fixed relay to use profiled database correctly
- Better error handling for empty tag sets

### 📦 Configuration

- **Configurable Seed Relays**: `SEED_RELAYS` environment variable
- **Worker Configuration**: Configurable via `WORKER_COUNT` and `WORKER_MULTIPLIER`
- **Profile Max Age**: Removed unused `PROFILE_MAX_AGE_DAYS` configuration

### 🔄 Refactoring

- Separated profiling and SQLite optimizations into distinct packages
- Removed unused relay connection semaphore
- Removed unused BatchProcessor code
- Cleaner eventstore implementation with channel-based operation queue
- Better separation of concerns across packages

### 📊 Monitoring

- Comprehensive debug logging for stats endpoint
- Worker statistics tracking
- Progress logging for profile refresh
- Detailed filter logging for QueryEvents errors

### 🐳 Docker & Deployment

- Updated Docker files to use `example.env` instead of `env.example`
- Tor now part of default production deployment
- Removed Prometheus and Grafana from production compose (simplified)
- Fixed volume mounts and Go version in Docker configs
- Added eventstore patch to Docker builds

---

## v1.2.0-production (2024-01-XX)

### 🚀 Major Features

- **Production-Ready Deployment**: Complete Docker-based production setup with health monitoring
- **Anonymous Access**: Built-in Tor proxy for privacy-preserving access
- **Auto-Healing**: Automatic container health monitoring and restart capabilities
- **Structured Logging**: Comprehensive logging with configurable levels
- **Health & Stats Endpoints**: Real-time monitoring endpoints for container orchestration

### 🔧 Performance Improvements

- **Database Optimizations**: Custom indexes and SQLite pragmas for better performance
- **Batch Processing**: Optimized event processing with configurable batch sizes
- **Memory Monitoring**: Real-time memory usage tracking and optimization
- **Worker Pool**: Multi-threaded event processing with configurable worker counts
- **Graceful Shutdown**: Proper cleanup and resource management during shutdown

### 🐳 Docker & Deployment

- **Multi-Stage Build**: Optimized production Dockerfile with minimal image size
- **Environment Configuration**: Centralized configuration via `.env` files
- **Volume Management**: Proper volume mounts for database and templates
- **Resource Limits**: Configurable memory and CPU limits
- **Health Checks**: Built-in health checks for container orchestration

### 🔒 Security & Privacy

- **Tor Integration**: Anonymous access via Tor proxy (enabled by default)
- **SSL Support**: Nginx reverse proxy with SSL termination
- **Security Headers**: Comprehensive security headers in Nginx configuration
- **Rate Limiting**: Built-in rate limiting for API endpoints

### 📊 Monitoring & Observability

- **Health Endpoint**: `/health` endpoint for container health checks
- **Stats Endpoint**: `/stats` endpoint with detailed metrics
- **Structured Logging**: JSON-formatted logs with configurable levels
- **Error Tracking**: Comprehensive error tracking and reporting
- **Timeout Detection**: Detection and logging of long-running processes

### 🛠️ Configuration

- **Environment Variables**: Extensive configuration via environment variables
- **Autoheal Settings**: Configurable health monitoring intervals
- **Database Paths**: Flexible database and template path configuration
- **Network Settings**: Configurable relay limits and network parameters

### 📁 File Structure

```
wot-relay/
├── docker-compose.prod.yml    # Production deployment
├── Dockerfile.prod            # Optimized production build
├── env.example               # Environment configuration template
├── nginx.conf                # Nginx reverse proxy configuration
├── deploy.sh                 # Automated deployment script
└── README.md                 # Comprehensive documentation
```

### 🔄 Migration from v1.1.0

1. **Update Configuration**: Copy `env.example` to `.env` and configure
2. **Deploy with Docker**: Use `docker-compose -f docker-compose.prod.yml up -d`
3. **Enable Tor**: Tor proxy is enabled by default for anonymous access
4. **Configure Nginx**: Use `--profile with-nginx` for SSL termination

### 🐛 Bug Fixes

- **Graceful Shutdown**: Resolved hanging during shutdown with proper context management
- **Channel Management**: Fixed race conditions in event processing
- **Memory Leaks**: Improved memory management and garbage collection
- **Query Limits**: Custom SQLite backend handles configurable query limits

### 📈 Performance Metrics

- **Startup Time**: ~30 seconds for full deployment
- **Memory Usage**: ~256MB base memory usage
- **Event Processing**: ~1000 events/second processing capacity
- **Health Check**: <1 second response time

### 🔗 Endpoints

- **Relay**: `wss://your-domain.com` (Nostr WebSocket)
- **Health**: `http://your-domain.com/health`
- **Stats**: `http://your-domain.com/stats`
- **Tor**: `http://your-onion-address.onion` (anonymous access)

### 📚 Documentation

- **README.md**: Updated with current features and quick start
- **README-PRODUCTION.md**: Comprehensive production deployment guide
- **env.example**: Complete environment variable reference
- **deploy.sh**: Automated deployment with validation

### 🎯 Next Steps

- Monitor health endpoints for container orchestration
- Configure SSL certificates for HTTPS access
- Set up automated backups for database
- Monitor logs for performance optimization

---

## v1.1.0-shutdown-fixes (2024-01-XX)

### 🐛 Bug Fixes
- Fixed graceful shutdown hanging on "stopping event processor"
- Resolved "panic: send on closed channel" during shutdown
- Improved context management for background processes

### 🔧 Improvements
- Enhanced EventProcessor with proper channel closure handling
- Added main context cancellation for coordinated shutdown
- Improved worker pool management

---

## v1.0.0 (2024-01-XX)

### 🎉 Initial Release
- Web of Trust relay implementation
- Basic Docker support
- Core Nostr relay functionality
