# Query Plan Analysis - Real Database

## Current Query Performance

### Query with Tags (Slow: 638ms)

**Filter:**
```json
{
  "kinds": [1984],
  "authors": ["58c741...", "7644...", "863f...", "c9f4...", "f275..."],
  "#p": ["35d2...", "6d5a...", "802b...", "8230..."],
  "since": 1759969515
}
```

**Current Query Plan:**
```
SEARCH tag0 USING COVERING INDEX tag_identifier_first_data_event_idx (identifier=? AND first_data=?)
SEARCH event USING INDEX ididx (id=?)
USE TEMP B-TREE FOR GROUP BY
USE TEMP B-TREE FOR ORDER BY
```

**Problem:**
1. ❌ **Starts with tag table** - scans all tags matching `#p` values
2. ❌ **Then looks up each event by ID** - no filtering by kind/authors yet
3. ❌ **Uses temp B-trees** for GROUP BY and ORDER BY (expensive!)
4. ❌ **Doesn't filter by kind=1984 and authors early**

This explains the 638ms query time!

---

### Query without Tags (Fast: ~20ms)

**Filter:**
```json
{
  "kinds": [1984],
  "authors": ["58c741...", "7644..."],
  "since": 1759969515
}
```

**Current Query Plan:**
```
SEARCH event USING INDEX kind_pubkey_time_idx (kind=? AND pubkey=? AND created_at>?)
USE TEMP B-TREE FOR ORDER BY
```

**Why it's fast:**
✅ Uses `kind_pubkey_time_idx` to quickly find matching events
✅ No tag JOIN needed
✅ Only one temp B-tree (for ORDER BY)

---

## Missing Indexes

**Your database doesn't have Migration 9 indexes yet!**

Migration 9 should have created:
- `event_kind1984_pubkey_time_idx` - Partial index for kind 1984
- `event_pubkey_kind_time_id_idx` - Covering index for pubkey+kind+time

**Current indexes on event table:**
- ✅ `kind_pubkey_time_idx` - Good for queries without tags
- ✅ `pubkey_kind_idx` - Helps with author+kind filtering
- ❌ Missing Migration 9 specialized indexes

---

## Solution

### 1. Apply Migration 9

**Restart your relay** to apply Migration 9. It will create:

```sql
-- Partial index for kind 1984 (small and fast)
CREATE INDEX event_kind1984_pubkey_time_idx 
ON event(pubkey, created_at DESC, id) WHERE kind = 1984

-- Covering index for pubkey+kind+time queries
CREATE INDEX event_pubkey_kind_time_id_idx 
ON event(pubkey, kind, created_at DESC, id)
```

### 2. Expected Query Plan After Migration 9

The query planner **should** choose a better path:
```
SEARCH event USING INDEX event_kind1984_pubkey_time_idx (pubkey=? AND created_at>?)
SEARCH tag0 USING INDEX tag_p_first_data_event_idx (first_data=? AND event_id=?)
GROUP BY and ORDER BY using index (no temp B-tree)
```

**Benefits:**
- Start with events (kind=1984 + authors) - much smaller set
- Then JOIN with tags (only the small result set)
- Use indexes for GROUP BY/ORDER BY (no temp B-trees)

### 3. Alternative: Optimize Query Logic

If Migration 9 doesn't help enough, consider:

**Option A: Split the query**
- First: Get events for kind=1984 + authors (fast)
- Then: Filter by `#p` tag in application code

**Option B: Change the filter order**
- Query by `#p` tag first (if very selective)
- Then filter by kind and authors

---

## Verification Steps

1. **Restart relay** to apply Migration 9
2. **Run query plan test:**
   ```bash
   go test -v ./pkg/newsqlite3 -run TestQueryPlan_Kind1984WithManyAuthorsAndTags
   ```
3. **Check for new indexes** in output
4. **Monitor slow query logs** - should drop to 10-50ms
5. **Check `/stats` endpoint** - `avg_db_ms` should decrease

---

## Key Findings

### Why Tag JOIN is Expensive

1. **Tag table is large** - many rows to scan
2. **No early filtering** - scans all matching tags before checking kind/authors
3. **Temp B-trees** - expensive sorting operations
4. **ID lookups** - one per tag match, then filtered down

### Why Migration 9 Helps

1. **Partial index on kind 1984** - Very small index, fast to scan
2. **Early filtering** - Reduces result set before tag JOIN
3. **Covering index** - All needed columns in index
4. **Better query plan** - SQLite chooses event table first

### Current Database State

- ✅ Tag indexes are good (`tag_p_first_data_event_idx`)
- ✅ Has `kind_pubkey_time_idx` (helps without tags)
- ❌ Missing Migration 9 specialized indexes
- ❌ Query planner not optimized for kind 1984 + tags

---

## Run Tests Again After Migration

```bash
# Test query plan with Migration 9
go test -v ./pkg/newsqlite3 -run TestQueryPlan

# Compare before/after
# Before: 638ms, starts with tag scan
# After: 10-50ms, starts with event filter
```

