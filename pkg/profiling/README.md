# Profiling Package

A performance profiling layer for Nostr event storage that wraps the `eventstore.Store` interface with detailed metrics, slow query detection, and semaphore-based concurrency control.

## Features

- **Performance Metrics**: Track calls, duration, and averages for all operations
- **Slow Query Detection**: Automatic logging of operations exceeding thresholds
- **Concurrency Control**: Semaphore-based write serialization and read throttling
- **Dual Timing**: Measures both total time (with queuing) and pure database time
- **Context Management**: Proper timeout and cancellation handling
- **Filter Validation**: Prevents problematic query patterns
- **Optional**: Can be disabled for maximum performance

## Architecture

The profiling layer wraps any `eventstore.Store` implementation:

```
Application → ProfiledEventStore → eventstore.Store (e.g., SQLite3Backend)
               ↓
            Metrics, Semaphores, Timeouts, Logging
```

When profiling is disabled, the application uses the store directly:

```
Application → eventstore.Store (direct access, maximum speed)
```

## Configuration

### Environment Variable

```bash
# Enable profiling (default: disabled)
ENABLE_PROFILING=TRUE

# Disable profiling for maximum performance
ENABLE_PROFILING=FALSE  # or unset
```

### Programmatic Setup

```go
import (
    "github.com/girino/wot-relay/pkg/profiling"
    "github.com/fiatjaf/eventstore"
)

// Create backend
backend := createBackend() // e.g., SQLite3Backend

// Wrap with profiling
profiled := profiling.NewProfiledEventStore(backend)

// Use profiled store
if err := profiled.Init(); err != nil {
    log.Fatal(err)
}
defer profiled.Close()
```

## Semaphore Configuration

The profiling layer uses semaphores to control concurrency:

```go
type ProfiledEventStore struct {
    Store          eventstore.Store
    WriteSemaphore chan struct{} // 1 slot (serialized writes)
    ReadSemaphore  chan struct{} // 1024 slots (concurrent reads)
}
```

- **Write Semaphore**: Ensures only 1 write operation at a time
- **Read Semaphore**: Allows up to 1024 concurrent read operations

This prevents database contention and ensures stable performance.

## Performance Metrics

### EventStoreStats Structure

```go
type EventStoreStats struct {
    // Total time (including queue time)
    SaveEventCalls       int64
    SaveEventDuration    time.Duration
    QueryEventsCalls     int64
    QueryEventsDuration  time.Duration
    DeleteEventCalls     int64
    DeleteEventDuration  time.Duration
    ReplaceEventCalls    int64
    ReplaceEventDuration time.Duration
    InitCalls            int64
    InitDuration         time.Duration
    CloseCalls           int64
    CloseDuration        time.Duration
    
    // Pure database time (excluding queue time)
    SaveEventDBDuration    time.Duration
    QueryEventsDBDuration  time.Duration
    DeleteEventDBDuration  time.Duration
    ReplaceEventDBDuration time.Duration
}
```

### Metrics Exposed

#### Total Time
- **Includes**: Semaphore wait time + database execution time
- **Perspective**: From the caller's point of view
- **Useful for**: Understanding overall latency

#### Database Time
- **Includes**: Only pure database execution time
- **Perspective**: From the worker's point of view
- **Useful for**: Database optimization

## API Reference

### Creating a Profiled Store

```go
func NewProfiledEventStore(backend eventstore.Store) *ProfiledEventStore
```

Creates a new profiled event store wrapping the given backend.

### Getting Statistics

```go
func (p *ProfiledEventStore) GetStats() EventStoreStats
```

Returns current performance statistics.

```go
func (p *ProfiledEventStore) ResetStats()
```

Resets all statistics to zero. Useful for measuring specific time periods.

```go
func (p *ProfiledEventStore) LogStats(logger interface{})
```

Logs statistics using the provided logger (must have `Info` method).

### Accessing Backend

```go
func (p *ProfiledEventStore) GetBackend() eventstore.Store
```

Returns the underlying eventstore backend.

### Standard eventstore.Store Methods

All standard methods are wrapped with profiling:

- `Init() error`
- `Close()`
- `SaveEvent(ctx context.Context, evt *nostr.Event) error`
- `QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error)`
- `DeleteEvent(ctx context.Context, evt *nostr.Event) error`
- `ReplaceEvent(ctx context.Context, evt *nostr.Event) error`

## Usage Examples

### Basic Usage

```go
// Create profiled store
profiled := profiling.NewProfiledEventStore(backend)
if err := profiled.Init(); err != nil {
    log.Fatal(err)
}
defer profiled.Close()

// Use normally
evt := &nostr.Event{...}
if err := profiled.SaveEvent(ctx, evt); err != nil {
    log.Printf("Save failed: %v", err)
}
```

### Monitoring Performance

```go
// Get current stats
stats := profiled.GetStats()

// Calculate averages
avgSaveMs := stats.SaveEventDuration.Milliseconds() / max(1, stats.SaveEventCalls)
avgQueryMs := stats.QueryEventsDuration.Milliseconds() / max(1, stats.QueryEventsCalls)

log.Printf("SaveEvent: %d calls, avg %dms", stats.SaveEventCalls, avgSaveMs)
log.Printf("QueryEvents: %d calls, avg %dms", stats.QueryEventsCalls, avgQueryMs)
```

### Periodic Stats Logging

```go
import "github.com/girino/wot-relay/pkg/logger"

ticker := time.NewTicker(5 * time.Minute)
defer ticker.Stop()

for range ticker.C {
    profiled.LogStats(logger.GetDefault())
    profiled.ResetStats() // Start fresh for next period
}
```

### Checking Semaphore Usage

```go
// Check if write semaphore is busy
writeBusy := len(profiled.WriteSemaphore)
writeCapacity := cap(profiled.WriteSemaphore)

// Check read semaphore usage
readUsed := len(profiled.ReadSemaphore)
readCapacity := cap(profiled.ReadSemaphore)

log.Printf("Write: %d/%d, Read: %d/%d", 
    writeBusy, writeCapacity, readUsed, readCapacity)
```

## Slow Query Detection

### Thresholds

The profiling layer automatically logs operations exceeding these thresholds:

- **SaveEvent**: > 200ms (total), > 100ms (DB only)
- **QueryEvents**: > 200ms (DB query time), > 1000ms (total)
- **DeleteEvent**: > 200ms (total), > 100ms (DB only)
- **ReplaceEvent**: > 200ms (total), > 100ms (DB only)

### Log Format

```
[WARN] [PROFILING] Slow SaveEvent (total) 
  {"duration": "350ms", "event_id": "abc123..."}

[WARN] [PROFILING] Slow SaveEvent (DB only) 
  {"duration": "150ms", "event_id": "abc123..."}

[WARN] [PROFILING] Slow QueryEvents 
  {"db_time": "250ms", "total_time": "1200ms", "event_count": 100, "filter": {...}}
```

## Context Management

### Automatic Timeouts

All operations have a 30-second timeout:

```go
// Automatically applied
queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

err := backend.SaveEvent(queryCtx, evt)
```

### Client Disconnect Handling

The profiling layer properly handles client disconnections:

```go
// If client cancels context (disconnects)
select {
case <-ctx.Done():
    // Log as DEBUG (normal behavior)
    logger.Debug("PROFILING", "QueryEvents canceled by client", ...)
    return
}
```

### Timeout Handling

```go
// If operation times out
select {
case <-timeoutCtx.Done():
    // Log as WARN (potential issue)
    logger.Warn("PROFILING", "QueryEvents timeout", ...)
    return
}
```

## Filter Validation

The profiling layer validates filters before executing queries:

### Empty Tag Sets

```go
// Removes empty tag arrays that cause errors
filter := nostr.Filter{
    Tags: nostr.TagMap{
        "p": []string{},  // Empty! Will be removed
        "e": []string{"abc123"},
    },
}
// After validation: only "e" tag remains
```

### Excessive Tag Values

```go
// Rejects filters with too many tag values
filter := nostr.Filter{
    Tags: nostr.TagMap{
        "p": make([]string, 2000),  // Too many!
    },
}
// Returns error: "too many tag values for tag 'p': 2000 (max 1000)"
```

### Excessive Authors

```go
// Rejects filters with too many authors
filter := nostr.Filter{
    Authors: make([]string, 2000),  // Too many!
}
// Returns error: "too many authors: 2000 (max 1000)"
```

## Stats Endpoint Integration

The profiling stats are exposed via the `/stats` endpoint:

```json
{
  "database": {
    "profiling_enabled": true,
    "operations": {
      "save_event": {
        "calls": 5000,
        "avg_ms": 15,
        "avg_db_ms": 12
      },
      "query_events": {
        "calls": 10000,
        "avg_ms": 50,
        "avg_db_ms": 35
      }
    },
    "semaphores": {
      "write_semaphore": {
        "used": 0,
        "capacity": 1
      },
      "read_semaphore": {
        "used": 12,
        "capacity": 1024
      }
    }
  }
}
```

## Performance Impact

### Overhead When Enabled

- **Semaphore acquisition**: ~microseconds per operation
- **Time measurement**: Negligible (uses `time.Now()`)
- **Stats aggregation**: Atomic operations (very fast)
- **Memory overhead**: ~100 bytes per ProfiledEventStore
- **Total overhead**: < 1% for most workloads

### Benefit When Disabled

- **No locking overhead**: Direct database access
- **No time tracking**: Saves a few nanoseconds per operation
- **Lower memory**: No stats structures
- **Simpler call stack**: Fewer function calls

## Use Cases

### When to Enable Profiling

1. **Initial Deployment**: Monitor performance for 24-48 hours
2. **Debugging**: Investigate slow queries or performance issues
3. **Capacity Planning**: Understand load patterns
4. **Optimization**: Measure impact of changes
5. **Development**: Always enable in dev/staging

### When to Disable Profiling

1. **Production**: After initial monitoring period
2. **High Load**: When every microsecond counts
3. **Low Resources**: Limited memory or CPU
4. **Stable Operation**: When performance is well-understood
5. **External Monitoring**: If using other profiling tools

## Advanced Features

### Customizing Semaphore Capacity

```go
// Create profiled store with custom read capacity
profiled := profiling.NewProfiledEventStore(backend)

// Adjust read semaphore capacity (default: 1024)
profiled.ReadSemaphore = make(chan struct{}, 512)  // Reduce to 512

// Write semaphore should always be 1 (serialized)
profiled.WriteSemaphore = make(chan struct{}, 1)
```

### Time Breakdown Analysis

The dual timing (total vs DB) helps identify bottlenecks:

```go
stats := profiled.GetStats()

totalTime := stats.SaveEventDuration.Milliseconds()
dbTime := stats.SaveEventDBDuration.Milliseconds()
queueTime := totalTime - dbTime

if queueTime > dbTime {
    log.Println("Bottleneck: Semaphore contention (increase workers?)")
} else {
    log.Println("Bottleneck: Database performance (optimize queries?)")
}
```

### Per-Operation Analysis

```go
func analyzeOperation(calls int64, totalDur, dbDur time.Duration) {
    if calls == 0 {
        return
    }
    
    avgTotal := totalDur.Milliseconds() / calls
    avgDB := dbDur.Milliseconds() / calls
    avgQueue := avgTotal - avgDB
    
    fmt.Printf("Avg total: %dms (queue: %dms, db: %dms)\n", 
        avgTotal, avgQueue, avgDB)
}

stats := profiled.GetStats()
analyzeOperation(stats.SaveEventCalls, 
    stats.SaveEventDuration, stats.SaveEventDBDuration)
```

## Testing

### Unit Tests

```go
func TestProfiledEventStore(t *testing.T) {
    // Create mock backend
    backend := &mockBackend{}
    
    // Create profiled store
    profiled := profiling.NewProfiledEventStore(backend)
    
    // Test operation
    ctx := context.Background()
    evt := &nostr.Event{...}
    
    if err := profiled.SaveEvent(ctx, evt); err != nil {
        t.Errorf("SaveEvent failed: %v", err)
    }
    
    // Check stats
    stats := profiled.GetStats()
    if stats.SaveEventCalls != 1 {
        t.Errorf("Expected 1 call, got %d", stats.SaveEventCalls)
    }
}
```

### Performance Benchmarks

```go
func BenchmarkProfiledSaveEvent(b *testing.B) {
    profiled := profiling.NewProfiledEventStore(backend)
    ctx := context.Background()
    evt := &nostr.Event{...}
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        profiled.SaveEvent(ctx, evt)
    }
}

func BenchmarkDirectSaveEvent(b *testing.B) {
    backend := createBackend()
    ctx := context.Background()
    evt := &nostr.Event{...}
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        backend.SaveEvent(ctx, evt)
    }
}
```

## Troubleshooting

### High Semaphore Contention

**Symptoms:**
- Large difference between total time and DB time
- Many goroutines waiting
- `read_semaphore_used` near capacity

**Solutions:**
1. Increase read semaphore capacity
2. Optimize queries to be faster
3. Add more database connections
4. Use caching layer

### Slow Queries Detected

**Symptoms:**
- Frequent "Slow QueryEvents" warnings
- High `avg_db_ms` in stats

**Solutions:**
1. Check database indexes
2. Run `ANALYZE` on database
3. Reduce query complexity
4. Add query-specific indexes

### High Memory Usage

**Symptoms:**
- Memory grows over time
- Many channels in use

**Solutions:**
1. Ensure contexts are canceled properly
2. Check for goroutine leaks
3. Reduce semaphore capacity
4. Monitor `GetStats()` calls

## Best Practices

1. **Enable Initially**: Start with profiling enabled for monitoring
2. **Monitor Regularly**: Check stats every 5-15 minutes
3. **Reset Periodically**: Call `ResetStats()` for accurate periods
4. **Log to Files**: Persist stats for analysis
5. **Disable in Production**: After 24-48 hours of stable operation
6. **Re-enable When Needed**: Turn back on for debugging
7. **Watch Semaphores**: High usage indicates bottlenecks
8. **Compare Timings**: Total vs DB time shows where delays occur

## Migration Path

### Phase 1: Enabled (First 24-48 hours)
```bash
ENABLE_PROFILING=TRUE
```
- Collect baseline metrics
- Identify slow queries
- Tune configuration
- Verify optimization

### Phase 2: Disabled (Production)
```bash
# Remove ENABLE_PROFILING or set to FALSE
```
- Maximum performance
- Monitor via connection stats
- Re-enable if issues arise

### Phase 3: Selective Enabling
- Enable during deployments
- Enable when investigating issues
- Enable for load testing
- Keep disabled otherwise

## See Also

- [Main README](../../README.md) - General relay documentation
- [pkg/logger](../logger/README.md) - Logging system used by profiling
- [pkg/sqlite3](../sqlite3/README.md) - SQLite backend that gets profiled

