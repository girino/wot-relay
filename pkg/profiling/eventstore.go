package profiling

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore"
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
	ReadSemaphore  chan struct{} // Concurrent reads (5 slots)
}

// NewProfiledEventStore creates a new ProfiledEventStore with semaphore-based concurrency
func NewProfiledEventStore(backend eventstore.Store) *ProfiledEventStore {
	return &ProfiledEventStore{
		Store:          backend,
		stats:          &EventStoreStats{},
		WriteSemaphore: make(chan struct{}, 1), // Serialized writes (1 slot)
		ReadSemaphore:  make(chan struct{}, 5), // Concurrent reads (5 slots)
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
			log.Printf("🐌 SLOW SaveEvent (total): %v (event %s)", duration, evt.ID)
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
		log.Printf("🐌 SLOW SaveEvent (DB only): %v (event %s)", dbDuration, evt.ID)
	}

	return err
}

// QueryEvents profiles the QueryEvents method with concurrent access
func (p *ProfiledEventStore) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {

	// Validate filter to prevent empty tag set errors
	if err := p.validateFilter(&filter); err != nil {
		log.Printf("❌ QueryEvents error: %v", err)
		log.Printf("🔍 Filter details: %+v", filter)
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, err
	}

	// Track performance - total time (including semaphore wait)
	start := time.Now()
	semaphoreAcquired := false

	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.QueryEventsCalls++
		p.stats.QueryEventsDuration += duration
		p.mutex.Unlock()

		// Release semaphore only if it was acquired
		if semaphoreAcquired {
			<-p.ReadSemaphore
		}

		if duration > 500*time.Millisecond {
			log.Printf("🐌 SLOW QueryEvents (total): %v (filter: %+v)", duration, filter)
		}
	}()

	// Acquire read semaphore for concurrency control (now part of total timing)
	select {
	case p.ReadSemaphore <- struct{}{}:
		// Got semaphore, proceed with query
		semaphoreAcquired = true
	case <-ctx.Done():
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		log.Printf("❌ QueryEvents context canceled: %v", ctx.Err())
		return closedCh, ctx.Err()
	}

	// Add a reasonable timeout for database queries to prevent indefinite blocking
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Execute query and measure pure DB time
	dbStart := time.Now()
	ch, err := p.Store.QueryEvents(queryCtx, filter)
	dbDuration := time.Since(dbStart)

	p.mutex.Lock()
	p.stats.QueryEventsDBDuration += dbDuration
	p.mutex.Unlock()

	// check if context timed out
	if queryCtx.Err() == context.DeadlineExceeded {
		log.Printf("⚠️ QueryEvents context timed out: %v", queryCtx.Err())
	}
	if queryCtx.Err() == context.Canceled {
		log.Printf("⚠️ QueryEvents context canceled: %v", queryCtx.Err())
	}
	if err != nil {
		log.Printf("❌ QueryEvents error: %v", err)
	}

	if dbDuration > 200*time.Millisecond {
		log.Printf("🐌 SLOW QueryEvents (DB only): %v (filter: %+v)", dbDuration, filter)
	}
	if err != nil {
		// Check if it's a context cancellation error
		if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
			log.Printf("⚠️ QueryEvents context canceled: %v", err)
		} else {
			log.Printf("❌ QueryEvents error: %v", err)
		}
		log.Printf("🔍 Filter details: %+v", filter)
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, err
	}

	return ch, nil
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
			log.Printf("🐌 SLOW DeleteEvent (total): %v (event %s)", duration, evt.ID)
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
		log.Printf("🐌 SLOW DeleteEvent (DB only): %v (event %s)", dbDuration, evt.ID)
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
			log.Printf("🐌 SLOW ReplaceEvent (total): %v (event %s)", duration, evt.ID)
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
		log.Printf("🐌 SLOW ReplaceEvent (DB only): %v (event %s)", dbDuration, evt.ID)
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

		log.Printf("📊 Init took: %v", duration)
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

		log.Printf("📊 Close took: %v", duration)
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

	log.Printf("📊 EventStore stats reset for new cycle")
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
				log.Printf("🧹 Removing empty tag '%s' from filter", tagName)
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
