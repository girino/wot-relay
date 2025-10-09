# Query Limits Configuration

## Overview

Database query limits can now be configured via environment variables to control the maximum complexity of queries allowed.

## Environment Variables

All variables are **optional** with defaults of **50** for each limit.

| Variable | Description | Default | Range |
|----------|-------------|---------|-------|
| `QUERY_IDS_LIMIT` | Max event IDs in a filter | 50 | 1-1000 |
| `QUERY_AUTHORS_LIMIT` | Max authors (pubkeys) in a filter | 50 | 1-1000 |
| `QUERY_KINDS_LIMIT` | Max event kinds in a filter | 50 | 1-100 |
| `QUERY_TAGS_LIMIT` | Max total tag values across all tags | 50 | 1-1000 |

## Configuration

### In .env file:

```bash
# Use defaults (50 for all limits)
# (no QUERY_*_LIMIT lines needed)

# Custom limits
QUERY_IDS_LIMIT=100
QUERY_AUTHORS_LIMIT=25
QUERY_KINDS_LIMIT=50
QUERY_TAGS_LIMIT=100
```

## Examples

### Query IDs Limit
```bash
# Allowed (within limit of 50)
{"ids": ["abc...", "def...", ... 50 IDs], "limit": 50}
✅ Allowed

# Rejected (exceeds limit)
{"ids": ["abc...", "def...", ... 100 IDs], "limit": 100}
❌ Rejected: "too many ids"
```

### Query Authors Limit
```bash
# Allowed (within limit of 50)
{"authors": ["abc...", "def...", ... 50 pubkeys], "limit": 100}
✅ Allowed

# Rejected (exceeds limit)
{"authors": ["abc...", "def...", ... 100 pubkeys], "limit": 100}
❌ Rejected: "too many authors"
```

### Query Kinds Limit
```bash
# Allowed (within limit of 50)
{"kinds": [1, 6, 7, 16, 30023, ... 50 kinds], "limit": 100}
✅ Allowed

# Rejected (exceeds limit)
{"kinds": [1, 2, 3, 4, 5, ... 100 kinds], "limit": 100}
❌ Rejected: "too many kinds"
```

### Query Tags Limit
```bash
# Allowed (4 + 8 = 12 total tag values, within limit of 50)
{"#p": ["abc...", "def...", "ghi...", "jkl..."],
 "#e": ["123...", "456...", "789...", "012...", "345...", "678...", "901...", "234..."],
 "limit": 100}
✅ Allowed

# Rejected (100 tag values exceeds limit)
{"#p": [100 pubkeys...], "limit": 100}
❌ Rejected: "too many tag values"
```

## Default Changes from Original Code

| Limit | Original Default | New Default | Change |
|-------|------------------|-------------|--------|
| IDs | 500 | **50** | ⬇️ 10x reduction |
| Authors | 500 | **50** | ⬇️ 10x reduction |
| Kinds | 10 | **50** | ⬆️ 5x increase |
| Tags | 10 | **50** | ⬆️ 5x increase |

## Why These Defaults?

### IDs & Authors: 50 (Reduced from 500)
- **Most queries use far fewer** - 50 is generous for normal use
- **Prevents expensive IN clauses** - Large author lists slow down queries
- **Still allows batch operations** - 50 events/authors at once is reasonable
- **Matches observed patterns** - Your slow queries had ~50 authors

### Kinds & Tags: 50 (Increased from 10)
- **Legitimate use cases** - File type queries need 12+ values
- **Multiple event types** - Apps often query 10+ kinds at once
- **Balanced** - Allows flexibility without performance issues
- **Tag JOINs optimized** - Migration 9 + index hints handle this well

## Performance Impact

### With Default Limits (50/50/50/50)
- ✅ Handles legitimate complex queries
- ✅ Prevents pathological cases (500 authors)
- ✅ Query optimization still effective
- ✅ Clear error messages when exceeded

### Custom Tuning

**For high-performance relays:**
```bash
QUERY_AUTHORS_LIMIT=100
QUERY_TAGS_LIMIT=100
```

**For resource-constrained relays:**
```bash
QUERY_AUTHORS_LIMIT=25
QUERY_TAGS_LIMIT=25
QUERY_KINDS_LIMIT=25
```

**For open data relays:**
```bash
QUERY_IDS_LIMIT=200
QUERY_AUTHORS_LIMIT=200
QUERY_KINDS_LIMIT=50
QUERY_TAGS_LIMIT=100
```

## Startup Log

When the relay starts, you'll see:
```
[INFO] [MAIN] Database query limits configured 
    {"ids_limit":50, "authors_limit":50, "kinds_limit":50, "tags_limit":50}
```

Or with custom values:
```
[INFO] [MAIN] Database query limits configured 
    {"ids_limit":100, "authors_limit":25, "kinds_limit":50, "tags_limit":100}
```

## Error Messages

When queries exceed limits, clients receive clear errors:

| Error | When | Solution |
|-------|------|----------|
| `too many ids` | IDs > QUERY_IDS_LIMIT | Split into multiple queries |
| `too many authors` | Authors > QUERY_AUTHORS_LIMIT | Reduce author list or split |
| `too many kinds` | Kinds > QUERY_KINDS_LIMIT | Query fewer kinds at once |
| `too many tag values` | Total tags > QUERY_TAGS_LIMIT | Reduce tag values or split |

## Monitoring

Check `/stats` endpoint to see if limits are being hit frequently:
```json
{
  "database": {
    "operations": {
      "query_events": {
        "calls": 1000,
        "avg_db_ms": 25
      }
    }
  }
}
```

If queries are consistently fast (<50ms), limits are working well.

## Testing

### Test Current Limits
```bash
# Start relay and check logs
./wot-relay

# Look for:
[INFO] [MAIN] Database query limits configured 
    {"ids_limit":50, "authors_limit":50, "kinds_limit":50, "tags_limit":50}
```

### Test Limit Enforcement
```bash
# Query with 51 authors (should fail)
{"authors": [51 pubkeys...], "limit": 100}
→ "too many authors"

# Query with 12 file types (should work)
{"kinds": [1063, 1065], "#m": [12 types...]}
✅ Allowed (was rejected with old limit of 10)
```

## Recommendations

### Start with Defaults
```bash
# Don't set any QUERY_*_LIMIT variables
# Use the sensible defaults of 50
```

### Monitor and Adjust
1. **Watch logs** for "too many" errors
2. **Check `/stats`** for query performance
3. **Adjust if needed** based on your use case

### For Most Relays
```bash
# Defaults are good:
QUERY_IDS_LIMIT=50
QUERY_AUTHORS_LIMIT=50
QUERY_KINDS_LIMIT=50
QUERY_TAGS_LIMIT=50
```

### For Special Cases

**Data archival relay:**
```bash
QUERY_IDS_LIMIT=200
QUERY_AUTHORS_LIMIT=100
```

**Simple relay (kinds 0,1,3 only):**
```bash
QUERY_AUTHORS_LIMIT=25
QUERY_KINDS_LIMIT=10
QUERY_TAGS_LIMIT=25
```

## Summary

✅ **All limits configurable** via environment variables  
✅ **Sensible defaults** of 50 for each limit  
✅ **Balanced approach** - flexible yet performant  
✅ **Clear error messages** when limits exceeded  
✅ **No .env changes needed** - works great with defaults  

**Recommended:** Start with defaults, adjust based on monitoring.

