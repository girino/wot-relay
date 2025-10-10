# sqlite3 Package

A high-performance SQLite3 backend for Nostr event storage with optimized indexes, query planning, and maintenance capabilities. This package wraps the `github.com/fiatjaf/eventstore` interface with additional optimizations.

## Features

- **Optimized Schema**: Custom table structure with separate tag table for efficient queries
- **Smart Indexing**: Covering indexes, partial indexes, and composite indexes for common query patterns
- **Migration System**: Automatic database schema migrations on startup
- **Query Optimization**: Index hints and query plan optimization
- **Slow Query Detection**: Configurable threshold for logging slow queries
- **Maintenance Operations**: Periodic ANALYZE and manual VACUUM support
- **Connection Pooling**: Configurable connection limits
- **Configurable Limits**: Control query complexity via environment variables

## Database Schema

### Main Tables

#### `event` Table
```sql
CREATE TABLE event (
    id TEXT NOT NULL,              -- Event ID (hex)
    pubkey TEXT NOT NULL,          -- Author's pubkey (hex)
    created_at INTEGER NOT NULL,   -- Unix timestamp
    kind INTEGER NOT NULL,         -- Event kind
    tags JSONB NOT NULL,           -- Original tags JSON (kept for compatibility)
    content TEXT NOT NULL,         -- Event content
    sig TEXT NOT NULL              -- Signature
)
```

#### `tag` Table (Normalized)
```sql
CREATE TABLE tag (
    event_id TEXT NOT NULL,        -- References event(id)
    tag_order INTEGER NOT NULL,    -- Tag position in array
    identifier TEXT NOT NULL,      -- Tag name (e.g., 'p', 'e')
    first_data TEXT,               -- First value (e.g., pubkey, event id)
    PRIMARY KEY (event_id, tag_order),
    FOREIGN KEY (event_id) REFERENCES event(id) ON DELETE CASCADE
)
```

#### `migrations` Table
```sql
CREATE TABLE migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)
```

### Key Indexes

#### Event Table Indexes
```sql
-- Unique ID lookup
CREATE UNIQUE INDEX ididx ON event(id)

-- Author lookups
CREATE INDEX pubkeyprefix ON event(pubkey)

-- Time-based queries
CREATE INDEX timeidx ON event(created_at DESC)

-- Kind-specific queries
CREATE INDEX kindidx ON event(kind)
CREATE INDEX kindtimeidx ON event(kind, created_at DESC)

-- Covering index for multi-kind queries
CREATE INDEX event_kind_time_id_idx ON event(kind, created_at DESC, id)

-- Optimized for specific kinds
CREATE INDEX event_kind1_time_id_idx ON event(created_at DESC, id) WHERE kind = 1
CREATE INDEX event_kind1984_pubkey_time_idx ON event(pubkey, created_at DESC, id) WHERE kind = 1984

-- Composite index for complex queries
CREATE INDEX event_pubkey_kind_time_id_idx ON event(pubkey, kind, created_at DESC, id)
```

#### Tag Table Indexes
```sql
-- Tag identifier lookups
CREATE INDEX tag_identifier_idx ON tag(identifier)

-- Event ID lookups (for JOINs)
CREATE INDEX tag_event_id_idx ON tag(event_id)

-- Composite indexes for tag queries
CREATE INDEX tag_identifier_first_data_idx ON tag(identifier, first_data)

-- Covering index (avoids table lookup)
CREATE INDEX tag_identifier_first_data_event_idx ON tag(identifier, first_data, event_id)

-- First data lookups
CREATE INDEX tag_first_data_idx ON tag(first_data)

-- Optimized for common tags
CREATE INDEX tag_p_first_data_event_idx ON tag(first_data, event_id) WHERE identifier = 'p'
CREATE INDEX tag_e_first_data_event_idx ON tag(first_data, event_id) WHERE identifier = 'e'
```

## Configuration

### SQLite3Backend Structure

```go
type SQLite3Backend struct {
    *sqlx.DB
    DatabaseURL         string        // Database connection URL with pragmas
    QueryLimit          int           // Max results per query (default: 100)
    QueryIDsLimit       int           // Max IDs in filter (default: 500)
    QueryAuthorsLimit   int           // Max authors in filter (default: 500)
    QueryKindsLimit     int           // Max kinds in filter (default: 10)
    QueryTagsLimit      int           // Max total tag values (default: 10)
    MaintenanceInterval time.Duration // Periodic ANALYZE interval
    SlowQueryThreshold  time.Duration // Log queries slower than this (default: 500ms)
}
```

### Environment Variables

Query limits can be configured via environment variables:

```bash
QUERY_LIMIT=100                    # Max results per query
QUERY_IDS_LIMIT=50                 # Max event IDs in filter
QUERY_AUTHORS_LIMIT=50             # Max authors in filter
QUERY_KINDS_LIMIT=50               # Max kinds in filter
QUERY_TAGS_LIMIT=50                # Max tag values in filter
```

## Usage

### Basic Setup

```go
import (
    "github.com/girino/wot-relay/pkg/sqlite3"
)

// Create backend
backend := &sqlite3.SQLite3Backend{
    DatabaseURL:       "relay.db?_journal_mode=WAL&_synchronous=NORMAL",
    QueryLimit:        100,
    QueryIDsLimit:     50,
    QueryAuthorsLimit: 50,
    QueryKindsLimit:   50,
    QueryTagsLimit:    50,
}

// Initialize (runs migrations, sets up indexes)
if err := backend.Init(); err != nil {
    log.Fatal(err)
}
defer backend.Close()
```

### With Periodic Maintenance

```go
backend := &sqlite3.SQLite3Backend{
    DatabaseURL:         "relay.db",
    MaintenanceInterval: 24 * time.Hour, // Run ANALYZE daily
}

if err := backend.Init(); err != nil {
    log.Fatal(err)
}
// Maintenance starts automatically
```

### Manual Maintenance

```go
// Run ANALYZE to update query planner statistics
if err := backend.RunMaintenance(); err != nil {
    log.Printf("Maintenance failed: %v", err)
}

// Vacuum to reclaim space (can be slow on large databases)
if err := backend.Vacuum(); err != nil {
    log.Printf("Vacuum failed: %v", err)
}
```

## Migration System

The package includes 10 migrations (0-9) that run automatically on first startup or when upgrading:

### Migration 0: Initial Schema
- Creates `event` table
- Creates basic indexes (id, pubkey, time, kind)

### Migration 1: Database Optimizations
- Enables WAL mode for better concurrency
- Sets synchronous mode to NORMAL
- Configures auto_vacuum

### Migration 2: Cleanup
- Removes old indexes from previous implementations

### Migration 3: Tag Normalization
- Creates `tag` table
- Migrates tags from `event.tags` JSONB to separate table
- Creates tag indexes

### Migration 4: Statistics
- Runs ANALYZE to update query planner

### Migration 5: Tag Value Index
- Adds index on `tag.first_data` for value-only queries

### Migration 6: Covering Indexes
- Adds covering indexes to avoid table lookups
- Creates partial indexes for common tags (`p`, `e`)
- Optimizes JOIN queries

### Migration 7: Schema Optimization
- Removes redundant `tag_data` column
- Reduces database size

### Migration 8: Statistics Update
- Runs ANALYZE after schema changes

### Migration 9: Kind 1984 Optimization
- Adds specialized indexes for kind 1984 (report) queries
- Optimizes queries with many authors and tags
- Covers complex multi-kind queries

### Migration Compatibility

The system detects existing databases from `fiatjaf/eventstore` and marks them at migration 0, then applies subsequent migrations incrementally.

## Query Optimization

### Index Hints

The package uses `INDEXED BY` hints to force SQLite to use optimal indexes:

```go
// For queries with authors, kinds, and tags
if len(joins) > 0 && len(filter.Authors) > 0 && len(filter.Kinds) > 0 {
    fromClause = "event INDEXED BY event_pubkey_kind_time_id_idx"
}
```

This prevents SQLite from choosing suboptimal query plans.

### Slow Query Detection

```go
backend.SlowQueryThreshold = 500 * time.Millisecond

// Queries slower than threshold are logged with:
// - Query text
// - Parameters
// - Execution time
// - Row count
// - Scan/send time breakdown
```

### Query Limits

Built-in protection against expensive queries:

```go
// Rejects queries exceeding configured limits
if len(filter.IDs) > b.QueryIDsLimit {
    return nil, nil, errors.New("too many ids")
}
if len(filter.Authors) > b.QueryAuthorsLimit {
    return nil, nil, errors.New("too many authors")
}
if len(filter.Kinds) > b.QueryKindsLimit {
    return nil, nil, errors.New("too many kinds")
}
// ... and more
```

## Performance Features

### Connection Optimization

```sql
-- Per-connection pragmas (set automatically)
PRAGMA cache_size = -64000            -- 64MB cache per connection
PRAGMA mmap_size = 268435456          -- 256MB memory-mapped I/O
PRAGMA busy_timeout = 5000            -- 5 second busy timeout
PRAGMA temp_store = MEMORY            -- In-memory temp tables
PRAGMA foreign_keys = ON              -- Enable FK constraints
```

### Persistent Optimizations

```sql
-- Database-level pragmas (persistent)
PRAGMA journal_mode = WAL             -- Write-Ahead Logging
PRAGMA synchronous = NORMAL           -- Safe but fast
PRAGMA auto_vacuum = INCREMENTAL      -- Gradual space reclamation
```

### Query Strategies

1. **Covering Indexes**: Avoid table lookups when possible
2. **Partial Indexes**: Smaller, faster indexes for specific conditions
3. **Index Hints**: Force optimal query plans
4. **Buffered Channels**: Reduce goroutine blocking
5. **Batch Operations**: Efficient bulk inserts

## API Reference

### Main Methods

#### Init
```go
func (b *SQLite3Backend) Init() error
```
Initializes the database, runs migrations, and starts maintenance if configured.

#### Close
```go
func (b *SQLite3Backend) Close()
```
Closes database connections and stops maintenance goroutine.

#### QueryEvents
```go
func (b *SQLite3Backend) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error)
```
Queries events matching the filter. Returns a channel that streams results.

#### CountEvents
```go
func (b *SQLite3Backend) CountEvents(ctx context.Context, filter nostr.Filter) (int64, error)
```
Counts events matching the filter without returning full events.

#### SaveEvent
```go
func (b *SQLite3Backend) SaveEvent(ctx context.Context, evt *nostr.Event) error
```
Saves an event to the database (upsert).

#### DeleteEvent
```go
func (b *SQLite3Backend) DeleteEvent(ctx context.Context, evt *nostr.Event) error
```
Deletes an event by ID.

#### ReplaceEvent
```go
func (b *SQLite3Backend) ReplaceEvent(ctx context.Context, evt *nostr.Event) error
```
Replaces events based on NIP-01 replacement rules.

### Maintenance Methods

#### RunMaintenance
```go
func (b *SQLite3Backend) RunMaintenance() error
```
Runs ANALYZE to update query planner statistics. Fast, safe to run frequently.

#### Vacuum
```go
func (b *SQLite3Backend) Vacuum() error
```
Rebuilds database to reclaim space. Slow, requires 2x disk space temporarily.

#### StartPeriodicMaintenance
```go
func (b *SQLite3Backend) StartPeriodicMaintenance()
```
Starts background goroutine that runs ANALYZE periodically (if `MaintenanceInterval` is set).

## Performance Considerations

### When to Run ANALYZE
- After bulk imports
- After deleting many events
- Periodically (daily is good)
- After schema changes

### When to Run VACUUM
- Database file is much larger than data size
- After deleting many events
- During maintenance windows (can take hours on large DBs)
- When you need to reclaim disk space

### Query Performance Tips

1. **Use specific filters**: More constraints = faster queries
2. **Limit results**: Use `limit` parameter
3. **Avoid broad tag queries**: Limit tag values
4. **Use time ranges**: `since` and `until` help use indexes
5. **Monitor slow queries**: Check logs for optimization opportunities

### Scaling Considerations

- **Database size**: Works well up to 100GB+
- **Concurrent reads**: Excellent (WAL mode)
- **Concurrent writes**: Limited to 1 writer (SQLite limitation)
- **Query complexity**: Limited by configured query limits
- **Memory usage**: Scales with connection pool and cache size

## Common Query Patterns

### Simple Author + Kind Query
```go
filter := nostr.Filter{
    Authors: []string{"abc123..."},
    Kinds:   []int{1},
    Limit:   50,
}
// Uses: event_pubkey_kind_time_id_idx
```

### Tag Query (Mentions)
```go
filter := nostr.Filter{
    Tags: nostr.TagMap{
        "p": []string{"def456..."},
    },
    Limit: 100,
}
// Uses: tag_p_first_data_event_idx (partial index)
```

### Complex Multi-Kind Query
```go
filter := nostr.Filter{
    Authors: []string{"abc123...", "def456..."},
    Kinds:   []int{1, 6, 7, 1984},
    Tags: nostr.TagMap{
        "p": []string{"xyz789..."},
    },
    Limit: 50,
}
// Uses: event_pubkey_kind_time_id_idx + tag covering indexes
// May use INDEXED BY hint for optimal plan
```

### Kind 1984 Reports Query
```go
filter := nostr.Filter{
    Kinds:   []int{1984},
    Authors: []string{"abc123...", /* ... 50 authors */},
    Tags: nostr.TagMap{
        "p": []string{"reported_user..."},
    },
    Limit: 100,
}
// Uses: event_kind1984_pubkey_time_idx (Migration 9 optimization)
```

## Troubleshooting

### Slow Queries

Check logs for `SLOW_QUERY` warnings:
```
[WARN] [SLOW_QUERY] QueryEvents slow execution 
  {"duration_ms": 850, "query": "...", "params": [...]}
```

**Solutions:**
1. Run `ANALYZE` to update statistics
2. Check if indexes are being used: `EXPLAIN QUERY PLAN ...`
3. Reduce query complexity (fewer authors/tags)
4. Add index for specific query pattern

### High Memory Usage

**Causes:**
- Large cache size (`cache_size` pragma)
- Large mmap size (`mmap_size` pragma)
- Many open connections

**Solutions:**
1. Reduce per-connection cache size
2. Reduce mmap_size
3. Limit max open connections

### Database Corruption

```bash
# Check integrity
sqlite3 relay.db "PRAGMA integrity_check;"

# If corrupted, restore from backup
# (Always maintain regular backups!)
```

### Migration Failures

Migrations are atomic. If one fails:
1. Check error message in logs
2. Fix underlying issue (disk space, permissions, etc.)
3. Restart relay (migrations resume from last successful version)

## Testing

### Run Query Tests
```bash
cd pkg/sqlite3
go test -v
```

### Test Specific Migration
```bash
go test -v -run TestMigration9
```

### Benchmark Queries
```bash
go test -bench=. -benchmem
```

## Best Practices

1. **Always use contexts**: Allows query cancellation
2. **Set reasonable limits**: Protect against expensive queries
3. **Monitor slow queries**: Identify optimization opportunities
4. **Run ANALYZE regularly**: Keep statistics fresh
5. **Backup before VACUUM**: It's a heavyweight operation
6. **Use WAL mode**: Much better for concurrent access
7. **Limit connection pool**: SQLite has limited write concurrency

## See Also

- [Main README](../../README.md) - General relay documentation
- [pkg/logger](../logger/README.md) - Logging used by this package
- [pkg/profiling](../profiling/README.md) - Performance profiling wrapper

