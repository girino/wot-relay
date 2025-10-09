# Query Optimization Summary

## Problem: Slow Kind 1984 Queries (638ms)

Queries for kind 1984 (reports) with multiple authors and `#p` tags were taking 200ms-1200ms.

## Root Cause

SQLite's query planner was choosing a suboptimal execution path:
1. ❌ Started with tag table scan (many rows)
2. ❌ Looked up events by ID one at a time  
3. ❌ Used temp B-trees for GROUP BY and ORDER BY
4. ❌ Didn't filter by kind/authors until late in the process

## Solution Applied

### 1. Migration 9: Added Specialized Indexes ✅

```sql
-- Partial index for kind 1984 queries
CREATE INDEX event_kind1984_pubkey_time_idx 
ON event(pubkey, created_at DESC, id) WHERE kind = 1984

-- Covering index for pubkey+kind+time queries  
CREATE INDEX event_pubkey_kind_time_id_idx 
ON event(pubkey, kind, created_at DESC, id)
```

### 2. Index Hints: Forced Better Query Plan ✅

Added `INDEXED BY` hints to force SQLite to use the right indexes:

```go
// For queries with authors, kinds, and tags
if len(joins) > 0 && len(filter.Authors) > 0 && len(filter.Kinds) > 0 {
    fromClause = "event INDEXED BY event_pubkey_kind_time_id_idx"
}
```

### 3. Logger Enhancement ✅

Fixed error logging to show actual error messages instead of empty objects:

```go
func normalizeFields(fields map[string]interface{}) map[string]interface{} {
    for key, value := range fields {
        if err, ok := value.(error); ok {
            normalized[key] = err.Error()
        }
    }
    return normalized
}
```

### 4. Semaphore Stats ✅

Added semaphore usage stats to `/stats` endpoint:

```json
{
  "database": {
    "semaphores": {
      "write_semaphore": {"used": 0, "capacity": 1},
      "read_semaphore": {"used": 5, "capacity": 32}
    }
  }
}
```

## Results

### Query Plan: Before
```
SEARCH tag0 ... (tag table first)
SEARCH event USING INDEX ididx (id=?)
USE TEMP B-TREE FOR GROUP BY
USE TEMP B-TREE FOR ORDER BY
```

### Query Plan: After
```
✅ SEARCH event USING INDEX event_pubkey_kind_time_id_idx (pubkey=? AND kind=? AND created_at>?)
✅ SEARCH tag0 USING COVERING INDEX tag_identifier_first_data_event_idx (...)
⚠️  USE TEMP B-TREE FOR GROUP BY (on filtered dataset)
⚠️  USE TEMP B-TREE FOR ORDER BY (on filtered dataset)
```

### Performance Improvement

**Expected improvement:** 200-1200ms → 10-100ms

**Key improvements:**
- Starts with event table (filters early)
- Smaller result set before JOIN
- Uses covering indexes (no table lookups)
- Temp B-trees operate on small dataset

## Files Changed

1. `pkg/newsqlite3/init.go` - Added Migration 9
2. `pkg/newsqlite3/query.go` - Added index hints
3. `pkg/logger/logger.go` - Fixed error serialization
4. `main.go` - Added semaphore stats to `/stats` endpoint
5. `pkg/profiling/eventstore.go` - Fixed error logging

## Testing

### Run Query Plan Test
```bash
go test -v ./pkg/newsqlite3 -run TestQueryPlan_Kind1984WithManyAuthorsAndTags
```

### Monitor Performance
1. Check `/stats` endpoint for query times
2. Watch logs for slow query warnings
3. Monitor semaphore usage

### Verify Indexes
```bash
# Connect to database
sqlite3 db/wot-relay.db

# Check indexes
.indexes event

# Should see:
# event_kind1984_pubkey_time_idx
# event_pubkey_kind_time_id_idx
```

## Tag Limit Policy

**Kept at 10** for performance reasons. Clients exceeding this limit will receive clear error messages:
```json
{"error": "too many tag values", "filter": {...}}
```

## Deployment

✅ All changes built and ready
✅ Binary: `wot-relay`
✅ Migration runs automatically on startup
✅ No downtime required

## Monitoring After Deployment

Watch for:
- ✅ Reduced slow query warnings for kind 1984
- ✅ Lower `avg_db_ms` in `/stats` endpoint
- ✅ Semaphore usage patterns
- ✅ Overall query performance improvement

## Rollback (if needed)

If issues occur:
1. Stop relay
2. Remove index hints from `query.go`
3. Rebuild and restart
4. Indexes remain but won't be forced

