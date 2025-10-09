# Profiling Layer Configuration

## Overview

The profiling layer can now be optionally enabled or disabled via environment variable. When disabled, the relay runs with direct database access for maximum performance.

## Environment Variable

**Variable:** `ENABLE_PROFILING` *(optional)*

**Values:**
- `TRUE` - Enable profiling layer (performance stats, semaphores, slow query detection)
- `FALSE` or *not set* - Disable profiling layer (default)

**Default:** Disabled (maximum performance)

## What the Profiling Layer Does

### When Enabled (`ENABLE_PROFILING=TRUE`)

The profiling layer wraps database operations with:

#### 1. Performance Metrics
Tracks detailed statistics for all database operations:
- **SaveEvent**: Total calls, duration, avg time
- **QueryEvents**: Total calls, duration, avg time  
- **DeleteEvent**: Total calls, duration, avg time
- **ReplaceEvent**: Total calls, duration, avg time

Exposed via `/stats` endpoint:
```json
{
  "database": {
    "profiling_enabled": true,
    "operations": {
      "query_events": {
        "calls": 1234,
        "total_ms": 45678,
        "avg_ms": 37.0,
        "avg_db_ms": 25.0
      }
    }
  }
}
```

#### 2. Semaphore-Based Concurrency Control
- **Write semaphore**: Serializes writes (1 at a time)
- **Read semaphore**: Limits concurrent reads (32 simultaneous)

Prevents database contention and ensures stable performance.

#### 3. Slow Query Detection
Logs queries that exceed thresholds:
- SaveEvent > 200ms → Warning
- QueryEvents > 200ms → Warning  
- DeleteEvent > 200ms → Warning

#### 4. Context Management
- 30-second timeout for all operations
- Proper client disconnect handling
- Cancellation tracking

### When Disabled (`ENABLE_PROFILING=FALSE` or not set)

Direct database access without wrapping:
- ✅ **Fastest performance** - No profiling overhead
- ✅ **Simpler stack** - Direct calls to database
- ✅ **Lower memory** - No stats tracking
- ❌ **No operation metrics** - Can't track performance
- ❌ **No semaphores** - Native SQLite concurrency only
- ❌ **No slow query warnings** - From profiling layer

**Still available when disabled:**
- ✅ Database connection stats
- ✅ Query limits enforcement  
- ✅ Index optimization (Migration 9)
- ✅ Rate limiters
- ✅ System metrics (memory, goroutines, etc.)

## Configuration

### In .env file:

```bash
# Option 1: Don't set the variable (RECOMMENDED - fastest)
# (no ENABLE_PROFILING line needed)

# Option 2: Explicitly disable (same as not setting it)
ENABLE_PROFILING=FALSE

# Option 3: Enable profiling (for monitoring and debugging)
ENABLE_PROFILING=TRUE
```

## Use Cases

### Disabled (Default - Recommended for Production)
**Best for:**
- Production relays prioritizing performance
- High-traffic relays
- Relays with external monitoring
- After optimization is complete

**Characteristics:**
- ✅ Maximum performance (no profiling overhead)
- ✅ Lower memory usage
- ✅ Simpler code path
- ℹ️ Monitor via connection stats only

### Enabled (Debugging & Monitoring)
**Best for:**
- Development environments
- Debugging performance issues
- Initial deployment monitoring
- Capacity planning

**Characteristics:**
- ℹ️ Detailed operation metrics
- ℹ️ Slow query warnings
- ℹ️ Semaphore usage tracking
- ⚠️ Slight performance overhead

## Startup Log

### Profiling Disabled (Default)
```
[INFO] [MAIN] Profiling layer disabled - using direct database access
```

### Profiling Enabled
```
[INFO] [MAIN] Profiling layer enabled
```

## Performance Impact

### Overhead When Enabled
- **Semaphore acquisition**: ~microseconds per operation
- **Time tracking**: Negligible  
- **Stats aggregation**: Atomic operations (very fast)
- **Total overhead**: < 1% for most workloads

### Benefit When Disabled
- **No locking overhead** from semaphores
- **No time measurement** overhead
- **Direct database calls** (fastest possible)
- **Lower goroutine count** (no profiling goroutines)

## Stats Endpoint Differences

### With Profiling Enabled
```json
{
  "database": {
    "profiling_enabled": true,
    "connections": {
      "open_connections": 5,
      "in_use": 2,
      "idle": 3
    },
    "semaphores": {
      "write_semaphore": {"used": 0, "capacity": 1},
      "read_semaphore": {"used": 5, "capacity": 32}
    },
    "operations": {
      "query_events": {
        "calls": 1234,
        "avg_ms": 37.0,
        "avg_db_ms": 25.0
      }
    }
  }
}
```

### With Profiling Disabled
```json
{
  "database": {
    "profiling_enabled": false,
    "connections": {
      "open_connections": 5,
      "in_use": 2,
      "idle": 3
    }
  }
}
```

## When to Enable Profiling

### Enable When:
- 🔍 Debugging slow queries
- 📊 Monitoring database performance
- 🔧 Tuning query limits
- 📈 Capacity planning
- 🐛 Investigating issues

### Disable When:
- 🚀 Optimizations are complete
- 💪 Running production workload
- ⚡ Maximum performance needed
- 📉 Low resource environment

## Migration Path

### Phase 1: Initial Deployment (Enable)
```bash
ENABLE_PROFILING=TRUE
```
- Monitor performance
- Identify slow queries
- Tune configuration
- Verify optimizations work

### Phase 2: Optimized Production (Disable)
```bash
# Remove ENABLE_PROFILING or set to FALSE
```
- Apply learned optimizations
- Run at maximum speed
- Monitor via connection stats
- Re-enable if issues arise

## Monitoring Without Profiling

Even with profiling disabled, you can monitor:

### 1. Connection Stats (Always Available)
```
/stats → database.connections
```
- Open connections
- In-use connections
- Idle connections
- Wait count

### 2. System Metrics (Always Available)
```
/stats → system
```
- Memory usage
- Goroutine count
- Active connections
- Event counts

### 3. External Tools
```bash
# SQLite CLI
sqlite3 db/wot-relay.db ".stats"

# System monitoring
htop, top, etc.
```

## Testing

### Test Profiling Status
```bash
# Start relay and check logs
./wot-relay

# Profiling disabled:
[INFO] [MAIN] Profiling layer disabled - using direct database access

# Profiling enabled:
[INFO] [MAIN] Profiling layer enabled
```

### Test /stats Endpoint
```bash
curl http://localhost:3334/stats | jq '.database.profiling_enabled'

# Disabled: false
# Enabled: true
```

## Performance Comparison

### Profiling Disabled (Default)
- Direct SQLite calls
- Native concurrency control
- No stats overhead
- **Fastest possible performance**

### Profiling Enabled
- Wrapped calls with metrics
- Semaphore-based concurrency
- Stats collection overhead
- ~1% slower, but detailed monitoring

## Recommendations

### For Most Relays
```bash
# Start with profiling enabled
ENABLE_PROFILING=TRUE

# After 24-48 hours of monitoring:
# - Verify optimizations work
# - Check for slow queries
# - Tune query limits if needed

# Then disable for production
# (remove ENABLE_PROFILING line)
```

### Quick Start (Skip Monitoring)
```bash
# Don't set ENABLE_PROFILING
# Use defaults with query optimization
# Monitor via connection stats only
```

## Summary

✅ **Optional profiling** - Enable only when needed  
✅ **Default disabled** - Maximum performance out of the box  
✅ **Easy toggle** - Just set env var  
✅ **No code changes** - Works transparently  
✅ **Stats always available** - Connection stats shown regardless  

**Recommended workflow:**
1. Start with `ENABLE_PROFILING=TRUE` for initial monitoring
2. Verify query performance is good
3. Disable profiling for production (`ENABLE_PROFILING=FALSE` or unset)
4. Re-enable temporarily if investigating issues

