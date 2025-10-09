# Query Optimization for Kind 1984 (Reports)

## Problem

Slow database queries (200ms - 1200ms) for kind 1984 events with:
- Large author lists (~50 pubkeys)
- `#p` tag filters (person being reported)
- `since` timestamp filters
- Often returning 0 results

Example slow query pattern:
```sql
SELECT event.* FROM event 
INNER JOIN tag AS tag0 ON tag0.event_id = event.id 
    AND tag0.identifier = 'p' 
    AND tag0.first_data IN (?)
WHERE event.pubkey IN (?, ?, ... 50 times)
  AND event.kind IN (1984)
  AND event.created_at >= ?
GROUP BY event.id, ...
ORDER BY event.created_at DESC, event.id 
LIMIT 100
```

## Root Cause

Without specialized indexes, SQLite's query planner may:
1. Start with the tag JOIN (scanning many rows)
2. Then filter by 50 authors (inefficient for empty result sets)
3. Finally filter by kind and timestamp

This is especially slow when no matching events exist, as SQLite must scan many rows before determining the result is empty.

## Solution: Migration 9

Added two specialized indexes:

### 1. Partial Index for Kind 1984
```sql
CREATE INDEX event_kind1984_pubkey_time_idx 
ON event(pubkey, created_at DESC, id) WHERE kind = 1984
```

**Purpose**: Optimizes queries that filter kind=1984 with multiple authors
- Only indexes kind 1984 events (partial index = smaller, faster)
- Allows fast lookup by pubkey
- Maintains time ordering for efficient `since` filtering
- Much smaller than a full table index

### 2. Composite Covering Index
```sql
CREATE INDEX event_pubkey_kind_time_id_idx 
ON event(pubkey, kind, created_at DESC, id)
```

**Purpose**: General-purpose index for pubkey+kind+time queries
- Covering index: includes all columns needed for the query
- Reduces table lookups when filtering by author and kind
- Supports ORDER BY created_at DESC efficiently

## How It Helps

With these indexes, SQLite can now:
1. **Start with the most selective condition** (kind 1984 + pubkey)
2. **Use the partial index** for fast author filtering
3. **Quickly determine empty results** without scanning tag table
4. **Efficiently handle large author lists** (50+ pubkeys)

Expected improvement: **200-1200ms → 10-50ms** for these queries

## Impact

- Faster responses for report queries (kind 1984)
- Reduced database load during peak traffic
- Better performance when many users query the same pubkey
- Minimal storage overhead (partial indexes are small)

## Verification

After deploying, monitor logs for:
- Reduced `db_time` in slow query warnings
- Fewer kind 1984 queries exceeding 200ms threshold
- Lower average query times in `/stats` endpoint

Check `/stats` endpoint for:
```json
{
  "database": {
    "operations": {
      "query_events": {
        "avg_db_ms": <should decrease>
      }
    }
  }
}
```

## Rollback (if needed)

If these indexes cause issues, they can be removed:
```sql
DROP INDEX IF EXISTS event_kind1984_pubkey_time_idx;
DROP INDEX IF EXISTS event_pubkey_kind_time_id_idx;
```

Then update `currentMigrationVersion` back to 8 in `pkg/newsqlite3/init.go`.

