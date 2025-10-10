# Release Notes - v1.5.0

**Release Date:** October 9, 2025  
**Release Type:** Stable Release  
**Previous Version:** v1.4.0

## 🎉 Overview

Version 1.5.0 is a major refactoring release focused on improving documentation, code organization, performance, and maintainability. This release includes a complete documentation overhaul, significant query performance improvements, and cleaner package architecture.

## ⚠️ Breaking Changes

### Complete SQLite Backend Rewrite
- `pkg/newsqlite3` has been completely rewritten as `pkg/sqlite3`
- **New Architecture**:
  - Normalized tag table (separate from event table)
  - All queries rewritten to avoid JSON parsing
  - Eliminated LIKE operations for better performance
  - 9-stage database migration system
- **Why This Matters**:
  - 10-20x faster queries (especially kind 1984)
  - More efficient storage (removed redundant tag_data)
  - Better query plan selection with specialized indexes
- If you're importing this package directly, update your imports:
  - Old: `github.com/girino/wot-relay/pkg/newsqlite3`
  - New: `github.com/girino/wot-relay/pkg/sqlite3`

### Removed Environment Variables
The following unused environment variables have been removed:
- `MAX_TRUST_NETWORK` (was set but never used)
- `MAX_ONE_HOP_NETWORK` (was set but never used)
- `MAX_RELAYS` (was set but never used)

If you had these in your `.env` file, they can be safely removed.

## ✨ Major Features

### 📚 Complete Documentation Overhaul
- **Unified README**: Consolidated all documentation into a single, comprehensive `README.md`
- **Package Documentation**: Created detailed READMEs for each internal package:
  - `pkg/logger/README.md` - Structured logging system
  - `pkg/sqlite3/README.md` - SQLite backend with 536 lines of documentation
  - `pkg/profiling/README.md` - Performance profiling layer (593 lines)
- **Removed Redundant Docs**: Deleted 9 redundant documentation files
- **Better Organization**: Clear sections for build, configuration, deployment, and troubleshooting

### 🚀 Query Performance Improvements
- **Migration 9**: New specialized indexes for kind 1984 (report) queries
  - `event_kind1984_pubkey_time_idx` - Partial index for kind 1984
  - `event_pubkey_kind_time_id_idx` - Composite covering index
- **Index Hints**: Force SQLite to use optimal query plans with `INDEXED BY`
- **Tag Table Optimization**: Removed redundant `tag_data` column, reducing database size
- **Query Limits**: Configurable via environment variables:
  - `QUERY_IDS_LIMIT` (default: 50)
  - `QUERY_AUTHORS_LIMIT` (default: 50)
  - `QUERY_KINDS_LIMIT` (default: 50)
  - `QUERY_TAGS_LIMIT` (default: 50)

### 🎯 Optional Filters
Three new optional filters can be enabled/disabled via environment variables:

#### Anti-Sync Bots Filter
- **Variable**: `ENABLE_ANTI_SYNC_BOTS_FILTER` (default: FALSE)
- **Purpose**: Detects and blocks automated scrapers
- **Behavior**: Requires authentication and WoT membership for bulk queries

#### Kind 1984 Filter
- **Variable**: `ENABLE_KIND1984_FILTER` (default: FALSE)
- **Purpose**: Restricts access to moderation/report events
- **Behavior**: Requires authentication and WoT membership for kind 1984 queries

#### Profiling Layer
- **Variable**: `ENABLE_PROFILING` (default: FALSE)
- **Purpose**: Detailed performance metrics and monitoring
- **Benefit**: Track operation statistics, slow queries, and semaphore usage

### 🔧 Database Improvements
- **Complete SQLite Backend Rewrite**: Ground-up implementation in `pkg/sqlite3`
  - **Normalized Schema**: Separate tag table instead of JSON column
  - **No JSON Parsing**: Direct column access for tag queries
  - **No LIKE Operations**: Exact matches on indexed columns
  - **Query Rewrite**: All queries optimized for the new schema
- **No More Patches**: Removed eventstore patches (no longer needed)
- **9 Database Migrations**: Automated schema evolution from old format
- **Covering Indexes**: Optimized indexes that avoid table lookups
- **Partial Indexes**: Specialized indexes for common query patterns (kind 1984, kind 0, kind 3)
- **Connection Pooling**: Configurable max connections and timeouts

## 🐛 Bug Fixes

### Logger Fixes
- **Duplicate Timestamps**: Fixed logger outputting timestamps twice
  - Before: `2025/10/09 21:39:59 [2025/10/09 21:39:59] INFO [PROFILES] ...`
  - After: `2025/10/09 21:39:59 INFO [PROFILES] ...`

### Query Performance
- **Slow Kind 1984 Queries**: Reduced from 200-1200ms to 10-100ms
- **Better Query Plans**: Index hints ensure optimal execution paths
- **Reduced Temp B-trees**: Operations on smaller filtered datasets

## 🔄 Refactoring

### SQLite Backend Rewrite
- **Complete Rewrite**: `pkg/sqlite3` with normalized schema
  - Replaced JSON tag storage with normalized tag table
  - Rewrote all queries to eliminate JSON parsing
  - Eliminated LIKE operations for exact indexed lookups
  - Added 9-stage migration system for backward compatibility
- **Removed**: `pkg/sqlite/` (unused wrapper)
- **Removed**: `patches/` directory (no longer needed with custom backend)
- **Organized**: Clear separation of concerns between packages

### Dependency Cleanup
- **Promoted to direct**: `jmoiron/sqlx`, `mattn/go-sqlite3`
- **Removed unused**: badger, xxhash, and other indirect dependencies
- **Kept**: `fiatjaf/eventstore` (still needed for Store interface)

### Dockerfile Improvements
- **Removed**: `patch` from build dependencies
- **Removed**: Patch copying and application steps
- **Simpler**: Cleaner, faster Docker builds

## 📈 Performance Improvements

### Concurrency
- **Read Semaphore**: Increased from 6 to 32 concurrent reads
- **Better Metrics**: Semaphore usage exposed in `/stats` endpoint
- **Human-Readable Durations**: Improved profiling log readability

### Database Optimizations
- **WAL Mode**: Better write concurrency
- **128MB Cache**: Increased from 64MB (2x improvement)
- **512MB mmap**: Increased from 256MB (2x improvement)
- **Slow Query Detection**: Configurable thresholds and logging

## 📝 Configuration Changes

### New Environment Variables
```bash
# Query Limits (all default to 50)
QUERY_IDS_LIMIT=50
QUERY_AUTHORS_LIMIT=50
QUERY_KINDS_LIMIT=50
QUERY_TAGS_LIMIT=50

# Optional Filters (all default to FALSE)
ENABLE_ANTI_SYNC_BOTS_FILTER=FALSE
ENABLE_KIND1984_FILTER=FALSE
ENABLE_PROFILING=FALSE
```

### Removed Environment Variables
```bash
# These were never actually used and have been removed
MAX_TRUST_NETWORK=40000        # REMOVED
MAX_ONE_HOP_NETWORK=50000      # REMOVED
MAX_RELAYS=1000                # REMOVED
```

## 📦 File Structure Changes

### Documentation Cleanup
**Deleted (9 files):**
- `ANTI_SYNC_BOTS_FILTER_CONFIG.md`
- `KIND1984_FILTER_CONFIG.md`
- `OPTIMIZATION_SUMMARY.md`
- `PROFILING_CONFIG.md`
- `QUERY_LIMITS_CONFIG.md`
- `QUERY_OPTIMIZATION.md`
- `QUERY_PLAN_ANALYSIS.md`
- `SLOW_QUERY_ANALYSIS.md`
- `README-PRODUCTION.md`

**Added (3 files):**
- `pkg/logger/README.md` (387 lines)
- `pkg/sqlite3/README.md` (536 lines)
- `pkg/profiling/README.md` (593 lines)

### Package Changes
**Renamed:**
- `pkg/newsqlite3/` → `pkg/sqlite3/`

**Removed:**
- `pkg/sqlite/` (unused wrapper)
- `patches/` (no longer needed)

## 🔍 Database Schema

### Current Migrations (9 total)
- **Migration 0**: Initial schema and indexes
- **Migration 1**: WAL mode and optimization pragmas
- **Migration 2**: Cleanup old indexes
- **Migration 3**: Tag table normalization
- **Migration 4**: ANALYZE statistics
- **Migration 5**: Tag value index
- **Migration 6**: Covering and partial indexes
- **Migration 7**: Remove tag_data column
- **Migration 8**: Post-optimization ANALYZE
- **Migration 9**: Kind 1984 query optimization (NEW)

## 📊 Statistics

### Code Changes
- **24 commits** since v1.4.0
- **16 files changed** in final commits
- **1,999 insertions** (+)
- **2,090 deletions** (-)
- **Net: -91 lines** (more efficient!)

### Documentation
- **Main README**: 654 lines (comprehensive)
- **Package docs**: 1,516 lines total
- **Total documentation**: 2,170+ lines

## 🚀 Upgrade Guide

### From v1.4.0 to v1.5.0

#### Step 1: Update Configuration
Remove unused variables from `.env`:
```bash
# Remove these lines (they were never used)
# MAX_TRUST_NETWORK=40000
# MAX_ONE_HOP_NETWORK=50000
# MAX_RELAYS=1000
```

#### Step 2: Optional - Enable New Features
Add these if you want the new optional features:
```bash
# Query limits (optional, defaults to 50)
QUERY_IDS_LIMIT=50
QUERY_AUTHORS_LIMIT=50
QUERY_KINDS_LIMIT=50
QUERY_TAGS_LIMIT=50

# Optional filters (disabled by default)
ENABLE_ANTI_SYNC_BOTS_FILTER=FALSE
ENABLE_KIND1984_FILTER=FALSE
ENABLE_PROFILING=FALSE
```

#### Step 3: Pull and Restart

**For Docker users:**
```bash
cd wot-relay
git pull
docker-compose -f docker-compose.prod.yml down
docker-compose -f docker-compose.prod.yml up -d --build
```

**For manual installation:**
```bash
cd wot-relay
git pull
go build -ldflags "-X main.version=$(git describe --tags --always)"
sudo systemctl restart wot-relay
```

#### Step 4: Verify
Check the version:
```bash
./wot-relay --version
```

Check the logs for any errors:
```bash
# Docker
docker-compose -f docker-compose.prod.yml logs -f wot-relay

# Systemd
sudo journalctl -u wot-relay -f
```

## 🎯 Recommended Settings

### For Most Relays
```bash
# Use defaults - they're well-tuned
# No changes needed!
```

### For High-Performance Relays
```bash
QUERY_AUTHORS_LIMIT=100
QUERY_TAGS_LIMIT=100
# Consider enabling profiling temporarily to monitor
ENABLE_PROFILING=TRUE
```

### For Private/WoT-Only Relays
```bash
ENABLE_ANTI_SYNC_BOTS_FILTER=TRUE
ENABLE_KIND1984_FILTER=TRUE
```

### For Memory-Constrained Systems
```bash
QUERY_AUTHORS_LIMIT=25
QUERY_TAGS_LIMIT=25
WOT_DEPTH=1
```

## 📖 Documentation Links

### Main Documentation
- [README.md](README.md) - Comprehensive guide (654 lines)
  - Build and run instructions
  - Complete configuration reference
  - Production deployment
  - Monitoring and troubleshooting

### Package Documentation
- [pkg/logger/README.md](pkg/logger/README.md) - Structured logging (387 lines)
- [pkg/sqlite3/README.md](pkg/sqlite3/README.md) - Database backend (536 lines)
- [pkg/profiling/README.md](pkg/profiling/README.md) - Performance profiling (593 lines)

### Quick Links
- **Configuration**: See README.md "Configuration" section
- **Production Setup**: See README.md "Production Deployment" section
- **Troubleshooting**: See README.md "Troubleshooting" section
- **API**: See README.md "API Endpoints" section

## 🙏 Acknowledgments

This release represents a significant cleanup and documentation effort to make the WoT Relay more maintainable, performant, and easier to use.

## 🐛 Known Issues

None at this time. Please report any issues!

## 🔗 Links

- **Repository**: https://github.com/bitvora/wot-relay
- **Issues**: https://github.com/bitvora/wot-relay/issues
- **Documentation**: See README.md

---

**Full Changelog**: https://github.com/bitvora/wot-relay/compare/v1.4.0...v1.5.0

