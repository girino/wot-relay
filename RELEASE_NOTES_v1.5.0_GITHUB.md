# v1.5.0 - Documentation & Performance Release

**Stable Release** - Major refactoring focused on documentation, performance, and code organization.

## 🎯 Highlights

- 📚 **Complete documentation overhaul** - Unified README with 1,500+ lines of new package docs
- 🚀 **10-20x faster kind 1984 queries** - Reduced from 200-1200ms to 10-100ms
- 🧹 **Cleaner codebase** - Removed 9 redundant docs, unused code, and unnecessary patches
- ⚙️ **Configurable query limits** - Fine-tune performance with environment variables
- 🎛️ **Optional filters** - Enable anti-sync bots, kind 1984 filtering, and profiling on demand

## ⚠️ Breaking Changes

### Package Renamed
- `pkg/newsqlite3` → `pkg/sqlite3` (update imports if you use this directly)

### Removed Environment Variables
These were never used and have been removed:
- `MAX_TRUST_NETWORK`
- `MAX_ONE_HOP_NETWORK`
- `MAX_RELAYS`

## ✨ New Features

### Query Limits Configuration
Control query complexity via environment variables (all default to 50):
```bash
QUERY_IDS_LIMIT=50
QUERY_AUTHORS_LIMIT=50
QUERY_KINDS_LIMIT=50
QUERY_TAGS_LIMIT=50
```

### Optional Filters
Enable/disable features as needed (all default to FALSE):
```bash
ENABLE_ANTI_SYNC_BOTS_FILTER=FALSE  # Block automated scrapers
ENABLE_KIND1984_FILTER=FALSE         # Restrict report queries to WoT
ENABLE_PROFILING=FALSE               # Detailed performance metrics
```

## 🚀 Performance Improvements

- **Migration 9**: New indexes for kind 1984 queries (10-20x faster)
- **32 concurrent reads**: Increased from 6 (5x improvement)
- **2x larger cache**: 128MB (was 64MB)
- **2x larger mmap**: 512MB (was 256MB)
- **Removed tag_data column**: Reduced database size
- **Index hints**: Force optimal query plans

## 🐛 Bug Fixes

- Fixed duplicate timestamps in logger output
- Optimized slow kind 1984 queries
- Improved query plan selection

## 📚 Documentation

### New Package Documentation
- `pkg/logger/README.md` - 387 lines covering structured logging
- `pkg/sqlite3/README.md` - 536 lines covering database backend
- `pkg/profiling/README.md` - 593 lines covering performance profiling

### Cleaned Up
Removed 9 redundant documentation files, consolidated into main README.md

## 🔄 Upgrade Instructions

### Quick Upgrade

**Docker:**
```bash
git pull
docker-compose -f docker-compose.prod.yml up -d --build
```

**Manual:**
```bash
git pull
go build -ldflags "-X main.version=$(git describe --tags --always)"
sudo systemctl restart wot-relay
```

### Optional: Clean Up .env
Remove these unused variables:
```bash
# MAX_TRUST_NETWORK=40000      # Remove this line
# MAX_ONE_HOP_NETWORK=50000    # Remove this line
# MAX_RELAYS=1000              # Remove this line
```

## 📊 Statistics

- **24 commits** since v1.4.0
- **1,999 insertions**, **2,090 deletions**
- **Net: -91 lines** (more efficient!)
- **2,170+ lines** of documentation

## 🔗 Full Details

See [RELEASE_NOTES_v1.5.0.md](RELEASE_NOTES_v1.5.0.md) for complete changelog and upgrade guide.

## 📦 Assets

- Source code (zip)
- Source code (tar.gz)

**Full Changelog**: https://github.com/bitvora/wot-relay/compare/v1.4.0...v1.5.0

