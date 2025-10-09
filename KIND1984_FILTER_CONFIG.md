# Kind 1984 Filter Configuration

## Overview

Kind 1984 (report/moderation events) filters can now be optionally enabled or disabled via environment variable.

## Environment Variable

**Variable:** `ENABLE_KIND1984_FILTER` *(optional)*

**Values:**
- `TRUE` - Enable kind 1984 filters (requires auth and WoT membership)
- `FALSE` or *not set* - Disable kind 1984 filters (default)

**Default:** Disabled (if variable is not present or set to anything other than `TRUE`)

## What the Filters Do (When Enabled)

### Filter 1: Query Complexity Check
Rejects kind 1984 queries that have **both** authors and tags:
```
If filter has kind 1984 AND authors AND tags:
  → Reject: "kind 1984 cannot have both tag and author filters"
```

**Why:** These queries are expensive due to the combination of multiple authors + tag JOINs.

### Filter 2: Authentication & WoT Check
Requires authentication and Web of Trust membership for kind 1984 queries:
```
If filter has kind 1984:
  1. Check if user is authenticated
     → If not: Reject with "auth-required: kind 1984 requires authentication"
  
  2. Check if authenticated pubkey is in Web of Trust
     → If not: Reject with "{pubkey} not in web of trust"
```

**Why:** Restricts access to reports/moderation data to trusted users only.

## Configuration

### In .env file:

```bash
# Option 1: Don't set the variable at all (default - filters disabled)
# (no ENABLE_KIND1984_FILTER line needed)

# Option 2: Explicitly disable (same as not setting it)
ENABLE_KIND1984_FILTER=FALSE

# Option 3: Enable kind 1984 filters (requires auth and WoT membership)
ENABLE_KIND1984_FILTER=TRUE
```

### Startup Log

When the relay starts, you'll see:
```
[INFO] [CONFIG] Kind 1984 filters disabled
```

or

```
[INFO] [CONFIG] Kind 1984 filters enabled
```

## Use Cases

### Disabled (Default)
- ✅ All kind 1984 queries allowed
- ✅ No authentication required
- ✅ Both authors+tags queries work
- ✅ Anyone can query reports/moderation data
- ⚠️ Potentially expensive queries allowed

**Best for:**
- Public relays serving moderation data
- Development/testing environments
- Relays with good query optimization (Migration 9 + index hints)

### Enabled
- ✅ Only authenticated WoT members can query kind 1984
- ✅ Prevents expensive authors+tags queries
- ✅ More control over who accesses report data
- ⚠️ May limit legitimate use cases

**Best for:**
- Private/semi-private relays
- Relays with limited resources
- Relays that want to restrict moderation data access

## Performance Impact

### With Filters Disabled (Default)
- Queries are optimized with Migration 9 indexes
- Index hints force better query plans
- Expected performance: 10-100ms for complex queries

### With Filters Enabled
- Blocks potentially expensive queries (authors+tags)
- Only allows simpler query patterns
- Adds authentication overhead

## Migration from Previous Behavior

**Previous:** Filters were always enabled and couldn't be disabled

**Now:** Filters are disabled by default but can be enabled

**To restore previous behavior:**
```bash
# In .env
ENABLE_KIND1984_FILTER=TRUE
```

## Query Optimization

Even with filters **disabled**, queries are optimized via:
1. ✅ Migration 9 specialized indexes
2. ✅ Index hints (`INDEXED BY`)
3. ✅ Better query plans

**Result:** Fast kind 1984 queries without restrictive filters.

## Testing

### Test Filter Status
```bash
# Start relay and check logs
./wot-relay

# Look for:
[INFO] [CONFIG] Kind 1984 filters disabled
# or
[INFO] [CONFIG] Kind 1984 filters enabled
```

### Test Behavior (Filters Disabled)
```bash
# All these should work:
# 1. Simple query
{"kinds": [1984], "limit": 10}

# 2. With authors
{"kinds": [1984], "authors": ["abc..."], "limit": 10}

# 3. With tags
{"kinds": [1984], "#p": ["xyz..."], "limit": 10}

# 4. With both (previously rejected)
{"kinds": [1984], "authors": ["abc..."], "#p": ["xyz..."], "limit": 10}
```

### Test Behavior (Filters Enabled)
```bash
# Without auth - should fail:
{"kinds": [1984], "limit": 10}
→ "auth-required: kind 1984 requires authentication"

# With authors AND tags - should fail:
{"kinds": [1984], "authors": ["abc..."], "#p": ["xyz..."], "limit": 10}
→ "kind 1984 cannot have both tag and author filters"
```

## Recommendations

### For Most Relays (Default)
```bash
ENABLE_KIND1984_FILTER=FALSE
```
- Let Migration 9 and index hints handle performance
- Allow all legitimate query patterns
- Monitor performance via `/stats` endpoint

### For Resource-Constrained Relays
```bash
ENABLE_KIND1984_FILTER=TRUE
```
- Restrict access to authenticated WoT members
- Block expensive query patterns
- Reduce database load

## Related Configuration

These work together with kind 1984 filters:
- `SLOW_QUERY_THRESHOLD` - Log slow queries (in newsqlite3)
- `READ_SEMAPHORE_CAPACITY` - Concurrent read queries (default: 32)
- `WRITE_SEMAPHORE_CAPACITY` - Concurrent write operations (default: 1)

