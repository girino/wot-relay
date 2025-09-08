package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/sqlite3"
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
	backend eventstore.Store
	stats   *EventStoreStats
	mutex   sync.RWMutex

	// Concurrency control using semaphores
	writeSemaphore chan struct{} // Serialized writes (1 slot)
	readSemaphore  chan struct{} // Concurrent reads (10 slots)
}

// NewProfiledEventStore creates a new ProfiledEventStore with semaphore-based concurrency
func NewProfiledEventStore(backend eventstore.Store) *ProfiledEventStore {
	return &ProfiledEventStore{
		backend:        backend,
		stats:          &EventStoreStats{},
		writeSemaphore: make(chan struct{}, 1),  // Serialized writes (1 slot)
		readSemaphore:  make(chan struct{}, 10), // Concurrent reads (10 slots)
	}
}

// GetBackend returns the underlying eventstore backend
func (p *ProfiledEventStore) GetBackend() eventstore.Store {
	return p.backend
}

// SaveEvent profiles the SaveEvent method
func (p *ProfiledEventStore) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	// Start timing from caller's perspective (includes semaphore wait time)
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.SaveEventCalls++
		p.stats.SaveEventDuration += duration
		p.mutex.Unlock()

		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW SaveEvent (total): %v (event %s)", duration, evt.ID)
		}
	}()

	// Acquire write semaphore for serialized access
	select {
	case p.writeSemaphore <- struct{}{}:
		// Got semaphore, proceed with write
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.writeSemaphore }() // Release semaphore

	// Execute database operation
	dbStart := time.Now()
	err := p.backend.SaveEvent(ctx, evt)
	dbDuration := time.Since(dbStart)

	p.mutex.Lock()
	p.stats.SaveEventDBDuration += dbDuration
	p.mutex.Unlock()

	if dbDuration > 50*time.Millisecond {
		log.Printf("🐌 SLOW SaveEvent (DB only): %v (event %s)", dbDuration, evt.ID)
	}

	return err
}

// QueryEvents profiles the QueryEvents method with concurrent access
func (p *ProfiledEventStore) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {

	// Acquire read semaphore for concurrency control
	select {
	case p.readSemaphore <- struct{}{}:
		// Got semaphore, proceed with query
	case <-ctx.Done():
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, ctx.Err()
	}

	// Track performance
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.QueryEventsCalls++
		p.stats.QueryEventsDuration += duration
		p.mutex.Unlock()

		// Release semaphore
		<-p.readSemaphore

		if duration > 500*time.Millisecond {
			log.Printf("🐌 SLOW QueryEvents: %v (filter: %+v)", duration, filter)
		}
	}()

	// Execute query directly with concurrency control
	ch, err := p.backend.QueryEvents(ctx, filter)
	if err != nil {
		log.Printf("❌ QueryEvents error: %v", err)
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
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.DeleteEventCalls++
		p.stats.DeleteEventDuration += duration
		p.mutex.Unlock()

		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW DeleteEvent (total): %v (event %s)", duration, evt.ID)
		}
	}()

	// Acquire write semaphore for serialized access
	select {
	case p.writeSemaphore <- struct{}{}:
		// Got semaphore, proceed with write
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.writeSemaphore }() // Release semaphore

	// Execute database operation
	dbStart := time.Now()
	err := p.backend.DeleteEvent(ctx, evt)
	dbDuration := time.Since(dbStart)

	p.mutex.Lock()
	p.stats.DeleteEventDBDuration += dbDuration
	p.mutex.Unlock()

	if dbDuration > 50*time.Millisecond {
		log.Printf("🐌 SLOW DeleteEvent (DB only): %v (event %s)", dbDuration, evt.ID)
	}

	return err
}

// ReplaceEvent profiles the ReplaceEvent method
func (p *ProfiledEventStore) ReplaceEvent(ctx context.Context, evt *nostr.Event) error {
	// Start timing from caller's perspective (includes semaphore wait time)
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.ReplaceEventCalls++
		p.stats.ReplaceEventDuration += duration
		p.mutex.Unlock()

		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW ReplaceEvent (total): %v (event %s)", duration, evt.ID)
		}
	}()

	// Acquire write semaphore for serialized access
	select {
	case p.writeSemaphore <- struct{}{}:
		// Got semaphore, proceed with write
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.writeSemaphore }() // Release semaphore

	// Execute database operation
	dbStart := time.Now()
	err := p.backend.ReplaceEvent(ctx, evt)
	dbDuration := time.Since(dbStart)

	p.mutex.Lock()
	p.stats.ReplaceEventDBDuration += dbDuration
	p.mutex.Unlock()

	if dbDuration > 50*time.Millisecond {
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

	return p.backend.Init()
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

	p.backend.Close()
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
	p.stats.DeleteEventDBDuration = 0
	p.stats.ReplaceEventDBDuration = 0

	log.Printf("📊 EventStore stats reset for new cycle")
}

// LogStats logs the current performance statistics
func (p *ProfiledEventStore) LogStats() {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	logger.Info("MONITOR", "EventStore Performance Stats", map[string]interface{}{
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
		"write_semaphore_used": len(p.writeSemaphore),
		"write_semaphore_cap":  cap(p.writeSemaphore),
		"read_semaphore_used":  len(p.readSemaphore),
		"read_semaphore_cap":   cap(p.readSemaphore),
	})
}

// Stats returns database statistics from the underlying backend
func (p *ProfiledEventStore) Stats() interface{} {
	if sqliteBackend, ok := p.backend.(*sqlite3.SQLite3Backend); ok {
		return sqliteBackend.Stats()
	}
	return nil
}
