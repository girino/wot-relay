# Logger Package

A structured logging package for the WoT Relay that provides JSON-formatted logs with configurable log levels and component-based organization.

## Features

- **Structured Logging**: JSON-formatted log entries for easy parsing
- **Log Levels**: DEBUG, INFO, WARN, ERROR
- **Component Tagging**: Organize logs by component/module
- **Field Support**: Attach structured data to log entries
- **Error Handling**: Automatic error serialization to strings
- **Default Logger**: Singleton pattern for easy global access

## Usage

### Basic Logging

```go
import "github.com/girino/wot-relay/pkg/logger"

// Using the default logger
logger.Info("MAIN", "Application started")
logger.Error("DATABASE", "Connection failed", map[string]interface{}{
    "error": err,
    "retry_count": 3,
})
```

### Creating a Custom Logger

```go
// Create logger with specific level
log := logger.NewLogger(logger.DEBUG)

// Set as default
logger.SetDefault(log)
```

### Log Levels

```go
// Available log levels (in order of severity)
logger.DEBUG  // Detailed debugging information
logger.INFO   // General informational messages
logger.WARN   // Warning messages
logger.ERROR  // Error messages

// Only logs at or above the configured level are output
log := logger.NewLogger(logger.INFO)
log.Debug("TEST", "This won't appear")    // Filtered out
log.Info("TEST", "This will appear")      // Shown
log.Warn("TEST", "This will appear")      // Shown
log.Error("TEST", "This will appear")     // Shown
```

### Component-Based Logging

Organize logs by component for better filtering and analysis:

```go
// Different components
logger.Info("MAIN", "Starting application")
logger.Info("DATABASE", "Connected successfully")
logger.Info("NETWORK", "Listening on port 3334")
logger.Warn("WOT", "Trust network size limit reached")
logger.Error("RELAY", "Failed to process event", map[string]interface{}{
    "event_id": eventID,
})
```

### Structured Fields

Attach additional data to log entries:

```go
logger.Info("ARCHIVING", "Completed archiving cycle", map[string]interface{}{
    "events_processed": 1000,
    "duration_ms": 5000,
    "new_events": 50,
})

logger.Error("QUERY", "Slow query detected", map[string]interface{}{
    "query_time_ms": 500,
    "filter": filter,
    "result_count": 100,
})
```

### Error Handling

Errors are automatically converted to strings for proper JSON serialization:

```go
err := someOperation()
if err != nil {
    logger.Error("OPERATION", "Operation failed", map[string]interface{}{
        "error": err,  // Automatically converted to error.Error()
    })
}
```

## Log Output Format

Logs are output in the following format:

```
[TIMESTAMP] LEVEL [COMPONENT] MESSAGE {JSON_FIELDS}
```

Example:
```
[2024/01/15 10:30:45] INFO [MAIN] Application started
[2024/01/15 10:30:46] ERROR [DATABASE] Connection failed {"error":"connection refused","retry_count":3}
[2024/01/15 10:30:50] WARN [WOT] Network size limit reached {"size":40000,"limit":40000}
```

## Configuration

### Environment Variable

The relay configures the logger based on the `LOG_LEVEL` environment variable:

```bash
# In .env
LOG_LEVEL=INFO    # DEBUG, INFO, WARN, or ERROR
```

### Programmatic Configuration

```go
// Set log level from environment
logLevel := logger.INFO
if level := os.Getenv("LOG_LEVEL"); level != "" {
    switch level {
    case "DEBUG":
        logLevel = logger.DEBUG
    case "WARN":
        logLevel = logger.WARN
    case "ERROR":
        logLevel = logger.ERROR
    default:
        logLevel = logger.INFO
    }
}

// Create and set default logger
log := logger.NewLogger(logLevel)
logger.SetDefault(log)
```

## Common Components

Standard component names used throughout the relay:

- `MAIN` - Main application lifecycle
- `DATABASE` - Database operations
- `SQLITE` - SQLite-specific operations
- `PROFILING` - Performance profiling
- `WOT` - Web of Trust operations
- `RELAY` - Relay operations
- `NETWORK` - Network operations
- `ARCHIVING` - Event archiving
- `MONITOR` - Monitoring and health checks
- `CONFIG` - Configuration loading
- `QUERY` - Database queries
- `WORKER` - Worker pool operations

## API Reference

### Types

```go
type LogLevel int

const (
    DEBUG LogLevel = iota
    INFO
    WARN
    ERROR
)

type Logger struct {
    level LogLevel
}
```

### Functions

#### NewLogger
```go
func NewLogger(level LogLevel) *Logger
```
Creates a new logger instance with the specified log level.

#### SetDefault
```go
func SetDefault(logger *Logger)
```
Sets the default global logger instance.

#### GetDefault
```go
func GetDefault() *Logger
```
Returns the default global logger instance. Creates one with INFO level if not set.

### Logger Methods

#### Log
```go
func (l *Logger) Log(level LogLevel, component, message string, fields ...map[string]interface{})
```
Logs a message at the specified level with optional structured fields.

#### Debug
```go
func (l *Logger) Debug(component, message string, fields ...map[string]interface{})
```
Logs a debug message.

#### Info
```go
func (l *Logger) Info(component, message string, fields ...map[string]interface{})
```
Logs an informational message.

#### Warn
```go
func (l *Logger) Warn(component, message string, fields ...map[string]interface{})
```
Logs a warning message.

#### Error
```go
func (l *Logger) Error(component, message string, fields ...map[string]interface{})
```
Logs an error message.

### Convenience Functions

Package-level functions that use the default logger:

```go
func Debug(component, message string, fields ...map[string]interface{})
func Info(component, message string, fields ...map[string]interface{})
func Warn(component, message string, fields ...map[string]interface{})
func Error(component, message string, fields ...map[string]interface{})
```

## Examples

### Application Startup
```go
logger.Info("MAIN", "WoT Relay starting", map[string]interface{}{
    "version": version,
    "config_path": configPath,
})
```

### Database Operations
```go
logger.Info("DATABASE", "Database initialized", map[string]interface{}{
    "path": dbPath,
    "size_mb": sizeInMB,
})

logger.Warn("DATABASE", "Slow query detected", map[string]interface{}{
    "duration_ms": duration.Milliseconds(),
    "query": queryString,
})
```

### Error Handling
```go
if err := relay.Start(); err != nil {
    logger.Error("RELAY", "Failed to start relay", map[string]interface{}{
        "error": err,
        "port": port,
    })
    return err
}
```

### Performance Monitoring
```go
start := time.Now()
// ... operation ...
duration := time.Since(start)

if duration > threshold {
    logger.Warn("PROFILING", "Slow operation", map[string]interface{}{
        "operation": "SaveEvent",
        "duration_ms": duration.Milliseconds(),
        "event_id": eventID,
    })
}
```

## Best Practices

1. **Use appropriate log levels**:
   - `DEBUG`: Detailed information for debugging
   - `INFO`: General operational information
   - `WARN`: Unusual but not error conditions
   - `ERROR`: Error conditions that need attention

2. **Include relevant context**:
   ```go
   // Good - includes context
   logger.Error("QUERY", "Query failed", map[string]interface{}{
       "error": err,
       "filter": filter,
       "duration_ms": duration,
   })
   
   // Bad - no context
   logger.Error("QUERY", "Query failed")
   ```

3. **Use consistent component names**:
   - Keep component names short and uppercase
   - Use established component names from the codebase
   - Group related logs under the same component

4. **Avoid sensitive data**:
   ```go
   // Bad - logs sensitive data
   logger.Info("AUTH", "User logged in", map[string]interface{}{
       "password": password,  // Never log passwords!
   })
   
   // Good - logs only necessary information
   logger.Info("AUTH", "User logged in", map[string]interface{}{
       "pubkey": pubkey,
   })
   ```

5. **Structure over concatenation**:
   ```go
   // Bad - string concatenation
   logger.Info("MAIN", "Processing " + strconv.Itoa(count) + " events")
   
   // Good - structured fields
   logger.Info("MAIN", "Processing events", map[string]interface{}{
       "count": count,
   })
   ```

## Testing

The logger uses Go's standard `log` package internally, making it easy to capture output in tests:

```go
func TestMyFunction(t *testing.T) {
    // Redirect log output
    var buf bytes.Buffer
    log.SetOutput(&buf)
    defer log.SetOutput(os.Stderr)
    
    // Run code that logs
    MyFunction()
    
    // Check log output
    output := buf.String()
    if !strings.Contains(output, "expected message") {
        t.Error("Expected log message not found")
    }
}
```

## Thread Safety

The logger is thread-safe and can be used concurrently from multiple goroutines. Each log call is atomic.

## Performance

- **Minimal overhead**: Only processes logs at or above configured level
- **Efficient JSON serialization**: Uses standard library encoding
- **No buffering**: Logs are written immediately (good for debugging, consider buffering for high-volume production use)

## See Also

- [Main README](../../README.md) - General relay documentation
- [pkg/profiling](../profiling/README.md) - Performance profiling that uses this logger
- [pkg/newsqlite3](../newsqlite3/README.md) - SQLite backend that uses this logger

