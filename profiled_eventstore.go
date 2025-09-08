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

// ProfiledEventStore wraps an eventstore with performance profiling and hybrid serialization
type ProfiledEventStore struct {
	backend eventstore.Store
	stats   *EventStoreStats
	mutex   sync.RWMutex

	// Write serialization (for SaveEvent, DeleteEvent, ReplaceEvent)
	writeOperationChan chan DatabaseOperation
	writeWorkerDone    chan struct{}
	writeWorkerWg      sync.WaitGroup

	// Read concurrency control (for QueryEvents)
	readSemaphore chan struct{}
}

// NewProfiledEventStore creates a new ProfiledEventStore with hybrid serialization
func NewProfiledEventStore(backend eventstore.Store) *ProfiledEventStore {
	p := &ProfiledEventStore{
		backend:            backend,
		stats:              &EventStoreStats{},
		writeOperationChan: make(chan DatabaseOperation, 1000), // Buffer for write operations
		writeWorkerDone:    make(chan struct{}),
		readSemaphore:      make(chan struct{}, 10), // Allow up to 10 concurrent reads
	}

	// Start the write serialization worker
	p.writeWorkerWg.Add(1)
	go p.writeSerializationWorker()

	return p
}

// writeSerializationWorker processes write operations one at a time
func (p *ProfiledEventStore) writeSerializationWorker() {
	defer p.writeWorkerWg.Done()

	for {
		select {
		case op := <-p.writeOperationChan:
			p.processWriteOperation(op)
		case <-p.writeWorkerDone:
			// Process remaining operations before shutdown
			for {
				select {
				case op := <-p.writeOperationChan:
					p.processWriteOperation(op)
				default:
					return
				}
			}
		}
	}
}

// processWriteOperation handles a single write database operation
func (p *ProfiledEventStore) processWriteOperation(op DatabaseOperation) {
	var err error
	var result interface{}

	switch op.Type {
	case OpSaveEvent:
		start := time.Now()
		err = p.backend.SaveEvent(context.Background(), op.SaveEvent)
		duration := time.Since(start)

		p.mutex.Lock()
		p.stats.SaveEventCalls++
		p.stats.SaveEventDuration += duration
		p.mutex.Unlock()

		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW SaveEvent: %v (event %s)", duration, op.SaveEvent.ID)
		}

	case OpDeleteEvent:
		start := time.Now()
		err = p.backend.DeleteEvent(context.Background(), op.DeleteEvent)
		duration := time.Since(start)

		p.mutex.Lock()
		p.stats.DeleteEventCalls++
		p.stats.DeleteEventDuration += duration
		p.mutex.Unlock()

		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW DeleteEvent: %v (event %s)", duration, op.DeleteEvent.ID)
		}

	case OpReplaceEvent:
		start := time.Now()
		err = p.backend.ReplaceEvent(context.Background(), op.ReplaceEvent)
		duration := time.Since(start)

		p.mutex.Lock()
		p.stats.ReplaceEventCalls++
		p.stats.ReplaceEventDuration += duration
		p.mutex.Unlock()

		if duration > 100*time.Millisecond {
			log.Printf("🐌 SLOW ReplaceEvent: %v (event %s)", duration, op.ReplaceEvent.ID)
		}

	case OpInit:
		start := time.Now()
		err = p.backend.Init()
		duration := time.Since(start)

		p.mutex.Lock()
		p.stats.InitCalls++
		p.stats.InitDuration += duration
		p.mutex.Unlock()

		log.Printf("📊 Init took: %v", duration)

	case OpClose:
		start := time.Now()
		p.backend.Close() // Close doesn't return error
		duration := time.Since(start)

		p.mutex.Lock()
		p.stats.CloseCalls++
		p.stats.CloseDuration += duration
		p.mutex.Unlock()

		log.Printf("📊 Close took: %v", duration)
	}

	// Send results back
	if op.ResultChan != nil {
		op.ResultChan <- result
		close(op.ResultChan)
	}
	if op.ErrorChan != nil {
		op.ErrorChan <- err
		close(op.ErrorChan)
	}
}

// GetBackend returns the underlying eventstore backend
func (p *ProfiledEventStore) GetBackend() eventstore.Store {
	return p.backend
}

// SaveEvent profiles the SaveEvent method
func (p *ProfiledEventStore) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	resultChan := make(chan interface{}, 1)
	errorChan := make(chan error, 1)

	op := DatabaseOperation{
		Type:       OpSaveEvent,
		SaveEvent:  evt,
		ResultChan: resultChan,
		ErrorChan:  errorChan,
	}

	select {
	case p.writeOperationChan <- op:
		// Operation queued successfully
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-errorChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, err
	}

	return ch, nil
}

// DeleteEvent profiles the DeleteEvent method
func (p *ProfiledEventStore) DeleteEvent(ctx context.Context, evt *nostr.Event) error {
	resultChan := make(chan interface{}, 1)
	errorChan := make(chan error, 1)

	op := DatabaseOperation{
		Type:        OpDeleteEvent,
		DeleteEvent: evt,
		ResultChan:  resultChan,
		ErrorChan:   errorChan,
	}

	select {
	case p.writeOperationChan <- op:
		// Operation queued successfully
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-errorChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ReplaceEvent profiles the ReplaceEvent method
func (p *ProfiledEventStore) ReplaceEvent(ctx context.Context, evt *nostr.Event) error {
	resultChan := make(chan interface{}, 1)
	errorChan := make(chan error, 1)

	op := DatabaseOperation{
		Type:         OpReplaceEvent,
		ReplaceEvent: evt,
		ResultChan:   resultChan,
		ErrorChan:    errorChan,
	}

	select {
	case p.writeOperationChan <- op:
		// Operation queued successfully
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-errorChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Init profiles the Init method
func (p *ProfiledEventStore) Init() error {
	resultChan := make(chan interface{}, 1)
	errorChan := make(chan error, 1)

	op := DatabaseOperation{
		Type:       OpInit,
		ResultChan: resultChan,
		ErrorChan:  errorChan,
	}

	select {
	case p.writeOperationChan <- op:
		// Operation queued successfully
	default:
		// If channel is full, process directly (for Init)
		return p.backend.Init()
	}

	select {
	case err := <-errorChan:
		return err
	default:
		return nil
	}
}

// Close profiles the Close method
func (p *ProfiledEventStore) Close() {
	// Signal write worker to stop
	close(p.writeWorkerDone)

	// Wait for write worker to finish
	p.writeWorkerWg.Wait()

	// Close the write operation channel
	close(p.writeOperationChan)

	// Call backend Close
	p.backend.Close()
}

// GetStats returns current performance statistics
func (p *ProfiledEventStore) GetStats() EventStoreStats {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return *p.stats
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
		"write_queue_size":     len(p.writeOperationChan),
		"write_queue_cap":      cap(p.writeOperationChan),
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
