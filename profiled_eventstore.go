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

// ProfiledEventStore wraps an eventstore with performance profiling
type ProfiledEventStore struct {
	backend eventstore.Store
	stats   *EventStoreStats
	mutex   sync.RWMutex
}

// EventStoreStats tracks performance metrics for each method
type EventStoreStats struct {
	SaveEventCalls     int64
	SaveEventDuration  time.Duration
	QueryEventsCalls   int64
	QueryEventsDuration time.Duration
	DeleteEventCalls   int64
	DeleteEventDuration time.Duration
	InitCalls          int64
	InitDuration       time.Duration
	CloseCalls         int64
	CloseDuration      time.Duration
}

// NewProfiledEventStore creates a new profiled eventstore wrapper
func NewProfiledEventStore(backend eventstore.Store) *ProfiledEventStore {
	return &ProfiledEventStore{
		backend: backend,
		stats:   &EventStoreStats{},
	}
}

// SaveEvent profiles the SaveEvent method
func (p *ProfiledEventStore) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.SaveEventCalls++
		p.stats.SaveEventDuration += duration
		p.mutex.Unlock()
		
		// Log slow operations
		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW SaveEvent: %v (event %s)", duration, evt.ID)
		}
	}()
	
	return p.backend.SaveEvent(ctx, evt)
}

// QueryEvents profiles the QueryEvents method
func (p *ProfiledEventStore) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.QueryEventsCalls++
		p.stats.QueryEventsDuration += duration
		p.mutex.Unlock()
		
		// Log slow operations
		if duration > 500*time.Millisecond {
			log.Printf("🐌 SLOW QueryEvents: %v (filter: %+v)", duration, filter)
		}
	}()
	
	// The backend QueryEvents returns a channel and error
	return p.backend.QueryEvents(ctx, filter)
}

// DeleteEvent profiles the DeleteEvent method
func (p *ProfiledEventStore) DeleteEvent(ctx context.Context, evt *nostr.Event) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.DeleteEventCalls++
		p.stats.DeleteEventDuration += duration
		p.mutex.Unlock()
		
		// Log slow operations
		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW DeleteEvent: %v (event %s)", duration, evt.ID)
		}
	}()
	
	return p.backend.DeleteEvent(ctx, evt)
}

// ReplaceEvent profiles the ReplaceEvent method
func (p *ProfiledEventStore) ReplaceEvent(ctx context.Context, evt *nostr.Event) error {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		p.mutex.Lock()
		p.stats.SaveEventCalls++ // Count as save event
		p.stats.SaveEventDuration += duration
		p.mutex.Unlock()
		
		// Log slow operations
		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW ReplaceEvent: %v (event %s)", duration, evt.ID)
		}
	}()
	
	return p.backend.ReplaceEvent(ctx, evt)
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

// LogStats logs current performance statistics
func (p *ProfiledEventStore) LogStats() {
	stats := p.GetStats()
	
	log.Printf("📊 EventStore Performance Stats:")
	log.Printf("  SaveEvent:   %d calls, avg: %v", stats.SaveEventCalls, p.avgDuration(stats.SaveEventDuration, stats.SaveEventCalls))
	log.Printf("  QueryEvents: %d calls, avg: %v", stats.QueryEventsCalls, p.avgDuration(stats.QueryEventsDuration, stats.QueryEventsCalls))
	log.Printf("  DeleteEvent: %d calls, avg: %v", stats.DeleteEventCalls, p.avgDuration(stats.DeleteEventDuration, stats.DeleteEventCalls))
	log.Printf("  Init:        %d calls, avg: %v", stats.InitCalls, p.avgDuration(stats.InitDuration, stats.InitCalls))
	log.Printf("  Close:       %d calls, avg: %v", stats.CloseCalls, p.avgDuration(stats.CloseDuration, stats.CloseCalls))
}

// avgDuration calculates average duration, handling division by zero
func (p *ProfiledEventStore) avgDuration(total time.Duration, calls int64) time.Duration {
	if calls == 0 {
		return 0
	}
	return total / time.Duration(calls)
}

// GetBackend returns the underlying backend for direct access if needed
func (p *ProfiledEventStore) GetBackend() eventstore.Store {
	return p.backend
}

// Stats returns database statistics from the underlying backend
func (p *ProfiledEventStore) Stats() interface{} {
	if sqliteBackend, ok := p.backend.(*sqlite3.SQLite3Backend); ok {
		return sqliteBackend.Stats()
	}
	return nil
}
