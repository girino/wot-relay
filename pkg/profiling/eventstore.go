package profiling

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/girino/wot-relay/pkg/logger"
	"github.com/nbd-wtf/go-nostr"
)

// EventStoreStats holds performance statistics for eventstore operations
type EventStoreStats struct {
	// Total time (including queue time) - from caller's perspective
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

	// Pure database time (excluding queue time) - from worker's perspective
	SaveEventDBDuration    time.Duration
	QueryEventsDBDuration  time.Duration
	DeleteEventDBDuration  time.Duration
	ReplaceEventDBDuration time.Duration
}

// OperationType represents the type of database operation
type OperationType int

const (
	OpSaveEvent OperationType = iota
	OpQueryEvents
	OpDeleteEvent
	OpReplaceEvent
	OpInit
	OpClose
)

// DatabaseOperation represents a serialized database operation
type DatabaseOperation struct {
	Type         OperationType
	SaveEvent    *nostr.Event
	QueryFilter  *nostr.Filter
	DeleteEvent  *nostr.Event
	ReplaceEvent *nostr.Event
	ResultChan   chan interface{}
	ErrorChan    chan error
}

// ProfiledEventStore wraps an eventstore with performance profiling and semaphore-based concurrency
type ProfiledEventStore struct {
	eventstore.Store
	stats *EventStoreStats
	mutex sync.RWMutex

	// Concurrency control using semaphores
	WriteSemaphore chan struct{} // Serialized writes (1 slot)
	ReadSemaphore  chan struct{} // Concurrent reads (6 slots)
}

// NewProfiledEventStore creates a new ProfiledEventStore with semaphore-based concurrency
func NewProfiledEventStore(backend eventstore.Store) *ProfiledEventStore {
	return &ProfiledEventStore{
		Store:          backend,
		stats:          &EventStoreStats{},
		WriteSemaphore: make(chan struct{}, 1), // Serialized writes (1 slot)
		ReadSemaphore:  make(chan struct{}, 6), // Concurrent reads (6 slots)
	}
}

// GetBackend returns the underlying eventstore backend
func (p *ProfiledEventStore) GetBackend() eventstore.Store {
	return p.Store
}

// SaveEvent profiles the SaveEvent method
func (p *ProfiledEventStore) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	// Start timing from caller's perspective (includes semaphore wait time)
	start := time.Now()
	semaphoreAcquired := false

	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.SaveEventCalls++
		p.stats.SaveEventDuration += duration
		p.mutex.Unlock()

		// Release semaphore only if it was acquired
		if semaphoreAcquired {
			<-p.WriteSemaphore
		}

		if duration > 200*time.Millisecond {
			logger.Warn("PROFILING", "Slow SaveEvent (total)", map[string]interface{}{"duration": humanizeDuration(duration), "event_id": evt.ID})
		}
	}()

	// Acquire write semaphore for serialized access
	select {
	case p.WriteSemaphore <- struct{}{}:
		// Got semaphore, proceed with write
		semaphoreAcquired = true
	case <-ctx.Done():
		return ctx.Err()
	}

	// Add a reasonable timeout for database operations
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Execute database operation
	dbStart := time.Now()
	err := p.Store.SaveEvent(queryCtx, evt)
	dbDuration := time.Since(dbStart)

	p.mutex.Lock()
	p.stats.SaveEventDBDuration += dbDuration
	p.mutex.Unlock()

	if dbDuration > 100*time.Millisecond {
		logger.Warn("PROFILING", "Slow SaveEvent (DB only)", map[string]interface{}{"duration": humanizeDuration(dbDuration), "event_id": evt.ID})
	}

	return err
}

func humanizeDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	} else if d < time.Millisecond {
		return fmt.Sprintf("%.0fµs", float64(d)/float64(time.Microsecond))
	} else if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	} else if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		min := int(d.Minutes())
		sec := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", min, sec)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dh%dm%ds", h, m, s)
}

// QueryEvents profiles the QueryEvents method with proper timing for async channels
func (p *ProfiledEventStore) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	// Validate filter to prevent empty tag set errors
	if err := p.validateFilter(&filter); err != nil {
		logger.Error("PROFILING", "QueryEvents validation failed", map[string]interface{}{"error": err, "filter": filter})
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, err
	}

	// Create the wrapper channel
	wrappedCh := make(chan *nostr.Event)

	go func() {
		defer close(wrappedCh)

		// Acquire semaphore to control concurrency (default: 6 simultaneous calls)
		select {
		case p.ReadSemaphore <- struct{}{}:
			// Got semaphore, proceed with query
		case <-ctx.Done():
			logger.Error("PROFILING", "QueryEvents context canceled before semaphore", map[string]interface{}{"error": ctx.Err()})
			return
		}

		// Release semaphore when goroutine completes
		defer func() {
			<-p.ReadSemaphore
		}()

		// Get the backend channel
		ch, err := p.Store.QueryEvents(ctx, filter)
		if err != nil {
			logger.Error("PROFILING", "QueryEvents backend error", map[string]interface{}{"error": err, "filter": filter})
			return
		}

		queryStart := time.Now()
		eventCount := 0
		firstEventTime := time.Time{}
		dbQueryTime := time.Duration(0)

		// Read all events from the backend channel
		for evt := range ch {
			eventCount++

			// Measure time to first event (true DB query time)
			if firstEventTime.IsZero() {
				firstEventTime = time.Now()
				dbQueryTime = firstEventTime.Sub(queryStart)

				if dbQueryTime > 200*time.Millisecond {
					logger.Warn("PROFILING", "Slow QueryEvents (DB time)", map[string]interface{}{"db_time": humanizeDuration(dbQueryTime), "filter": filter})
				}
			}

			wrappedCh <- evt
		}

		// Calculate total time for the complete operation
		totalTime := time.Since(queryStart)

		// If no events were returned, dbQueryTime is the same as totalTime
		if eventCount == 0 {
			dbQueryTime = totalTime
		}

		// Update stats with both metrics
		p.mutex.Lock()
		p.stats.QueryEventsCalls++
		p.stats.QueryEventsDuration += totalTime     // Total operation time
		p.stats.QueryEventsDBDuration += dbQueryTime // Time to first event (DB query time)
		p.mutex.Unlock()

		// Log slow queries based on DB query time (time to first event)
		if totalTime > 1000*time.Millisecond || dbQueryTime > 200*time.Millisecond {
			logger.Warn("PROFILING", "Slow QueryEvents", map[string]interface{}{
				"db_time":     humanizeDuration(dbQueryTime),
				"total_time":  humanizeDuration(totalTime),
				"event_count": eventCount,
				"filter":      filter,
			})
		}
	}()

	return wrappedCh, nil
}

// DeleteEvent profiles the DeleteEvent method
func (p *ProfiledEventStore) DeleteEvent(ctx context.Context, evt *nostr.Event) error {
	// Start timing from caller's perspective (includes semaphore wait time)
	start := time.Now()
	semaphoreAcquired := false

	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.DeleteEventCalls++
		p.stats.DeleteEventDuration += duration
		p.mutex.Unlock()

		// Release semaphore only if it was acquired
		if semaphoreAcquired {
			<-p.WriteSemaphore
		}

		if duration > 200*time.Millisecond {
			logger.Warn("PROFILING", "Slow DeleteEvent (total)", map[string]interface{}{"duration": humanizeDuration(duration), "event_id": evt.ID})
		}
	}()

	// Acquire write semaphore for serialized access
	select {
	case p.WriteSemaphore <- struct{}{}:
		// Got semaphore, proceed with write
		semaphoreAcquired = true
	case <-ctx.Done():
		return ctx.Err()
	}

	// Add a reasonable timeout for database operations
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Execute database operation
	dbStart := time.Now()
	err := p.Store.DeleteEvent(queryCtx, evt)
	dbDuration := time.Since(dbStart)

	p.mutex.Lock()
	p.stats.DeleteEventDBDuration += dbDuration
	p.mutex.Unlock()

	if dbDuration > 100*time.Millisecond {
		logger.Warn("PROFILING", "Slow DeleteEvent (DB only)", map[string]interface{}{"duration": humanizeDuration(dbDuration), "event_id": evt.ID})
	}

	return err
}

// ReplaceEvent profiles the ReplaceEvent method
func (p *ProfiledEventStore) ReplaceEvent(ctx context.Context, evt *nostr.Event) error {
	// Start timing from caller's perspective (includes semaphore wait time)
	start := time.Now()
	semaphoreAcquired := false

	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.ReplaceEventCalls++
		p.stats.ReplaceEventDuration += duration
		p.mutex.Unlock()

		// Release semaphore only if it was acquired
		if semaphoreAcquired {
			<-p.WriteSemaphore
		}

		if duration > 200*time.Millisecond {
			logger.Warn("PROFILING", "Slow ReplaceEvent (total)", map[string]interface{}{"duration": humanizeDuration(duration), "event_id": evt.ID})
		}
	}()

	// Acquire write semaphore for serialized access
	select {
	case p.WriteSemaphore <- struct{}{}:
		// Got semaphore, proceed with write
		semaphoreAcquired = true
	case <-ctx.Done():
		return ctx.Err()
	}

	// Add a reasonable timeout for database operations
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Execute database operation
	dbStart := time.Now()
	err := p.Store.ReplaceEvent(queryCtx, evt)
	dbDuration := time.Since(dbStart)

	p.mutex.Lock()
	p.stats.ReplaceEventDBDuration += dbDuration
	p.mutex.Unlock()

	if dbDuration > 100*time.Millisecond {
		logger.Warn("PROFILING", "Slow ReplaceEvent (DB only)", map[string]interface{}{"duration": humanizeDuration(dbDuration), "event_id": evt.ID})
	}

	return err
}

// Init profiles the Init method
func (p *ProfiledEventStore) Init() error {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.InitCalls++
		p.stats.InitDuration += duration
		p.mutex.Unlock()

		logger.Info("PROFILING", "Init completed", map[string]interface{}{"duration": humanizeDuration(duration)})
	}()

	return p.Store.Init()
}

// Close profiles the Close method
func (p *ProfiledEventStore) Close() {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.CloseCalls++
		p.stats.CloseDuration += duration
		p.mutex.Unlock()

		logger.Info("PROFILING", "Close completed", map[string]interface{}{"duration": humanizeDuration(duration)})
	}()

	p.Store.Close()
}

// GetStats returns current performance statistics
func (p *ProfiledEventStore) GetStats() EventStoreStats {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return *p.stats
}

// ResetStats resets all performance statistics to zero
func (p *ProfiledEventStore) ResetStats() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Reset all counters and durations
	p.stats.SaveEventCalls = 0
	p.stats.SaveEventDuration = 0
	p.stats.QueryEventsCalls = 0
	p.stats.QueryEventsDuration = 0
	p.stats.DeleteEventCalls = 0
	p.stats.DeleteEventDuration = 0
	p.stats.ReplaceEventCalls = 0
	p.stats.ReplaceEventDuration = 0
	p.stats.InitCalls = 0
	p.stats.InitDuration = 0
	p.stats.CloseCalls = 0
	p.stats.CloseDuration = 0

	// Reset pure database timing
	p.stats.SaveEventDBDuration = 0
	p.stats.QueryEventsDBDuration = 0
	p.stats.DeleteEventDBDuration = 0
	p.stats.ReplaceEventDBDuration = 0

	logger.Info("PROFILING", "EventStore stats reset for new cycle")
}

// LogStats logs the current performance statistics
func (p *ProfiledEventStore) LogStats(logger interface{}) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// Use type assertion to check if logger has Info method
	if l, ok := logger.(interface {
		Info(component, message string, fields ...map[string]interface{})
	}); ok {
		l.Info("MONITOR", "EventStore Performance Stats", map[string]interface{}{
			// Total time (including semaphore wait time)
			"save_event_calls":     p.stats.SaveEventCalls,
			"save_event_avg_ms":    p.stats.SaveEventDuration.Milliseconds() / max(1, p.stats.SaveEventCalls),
			"query_events_calls":   p.stats.QueryEventsCalls,
			"query_events_avg_ms":  p.stats.QueryEventsDuration.Milliseconds() / max(1, p.stats.QueryEventsCalls),
			"delete_event_calls":   p.stats.DeleteEventCalls,
			"delete_event_avg_ms":  p.stats.DeleteEventDuration.Milliseconds() / max(1, p.stats.DeleteEventCalls),
			"replace_event_calls":  p.stats.ReplaceEventCalls,
			"replace_event_avg_ms": p.stats.ReplaceEventDuration.Milliseconds() / max(1, p.stats.ReplaceEventCalls),
			"init_calls":           p.stats.InitCalls,
			"init_avg_ms":          p.stats.InitDuration.Milliseconds() / max(1, p.stats.InitCalls),
			"close_calls":          p.stats.CloseCalls,
			"close_avg_ms":         p.stats.CloseDuration.Milliseconds() / max(1, p.stats.CloseCalls),

			// Pure database time (excluding semaphore wait time)
			"save_event_db_avg_ms":    p.stats.SaveEventDBDuration.Milliseconds() / max(1, p.stats.SaveEventCalls),
			"query_events_db_avg_ms":  p.stats.QueryEventsDBDuration.Milliseconds() / max(1, p.stats.QueryEventsCalls),
			"delete_event_db_avg_ms":  p.stats.DeleteEventDBDuration.Milliseconds() / max(1, p.stats.DeleteEventCalls),
			"replace_event_db_avg_ms": p.stats.ReplaceEventDBDuration.Milliseconds() / max(1, p.stats.ReplaceEventCalls),

			// Semaphore usage
			"write_semaphore_used": len(p.WriteSemaphore),
			"write_semaphore_cap":  cap(p.WriteSemaphore),
			"read_semaphore_used":  len(p.ReadSemaphore),
			"read_semaphore_cap":   cap(p.ReadSemaphore),
		})
	}
}

// Stats returns database statistics from the underlying backend
func (p *ProfiledEventStore) Stats() interface{} {
	// Check if the backend has a Stats method
	if statsBackend, ok := p.Store.(interface{ Stats() interface{} }); ok {
		return statsBackend.Stats()
	}
	return nil
}

// validateFilter checks for problematic filter configurations and cleans them up
func (p *ProfiledEventStore) validateFilter(filter *nostr.Filter) error {
	// Remove empty tag arrays that cause "empty tag set" errors
	if len(filter.Tags) > 0 {
		for tagName, tagValues := range filter.Tags {
			if len(tagValues) == 0 {
				logger.Debug("PROFILING", "Removing empty tag from filter", map[string]interface{}{"tag_name": tagName})
				delete(filter.Tags, tagName)
			}
		}
	}

	// Check for too many tag values that exceed SQLite limits
	for tagName, tagValues := range filter.Tags {
		if len(tagValues) > 1000 { // SQLite practical limit
			return fmt.Errorf("too many tag values for tag '%s': %d (max 1000)", tagName, len(tagValues))
		}
	}

	// Check for too many authors
	if len(filter.Authors) > 1000 {
		return fmt.Errorf("too many authors: %d (max 1000)", len(filter.Authors))
	}

	return nil
}

// max returns the maximum of two int64 values
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
