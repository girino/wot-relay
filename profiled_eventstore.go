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

// ProfiledEventStore wraps an eventstore with performance profiling and serialization
type ProfiledEventStore struct {
	backend eventstore.Store
	stats   *EventStoreStats
	mutex   sync.RWMutex

	// Serialization channels
	operationChan chan DatabaseOperation
	workerDone    chan struct{}
	workerWg      sync.WaitGroup
}

// NewProfiledEventStore creates a new ProfiledEventStore with serialized operations
func NewProfiledEventStore(backend eventstore.Store) *ProfiledEventStore {
	p := &ProfiledEventStore{
		backend:       backend,
		stats:         &EventStoreStats{},
		operationChan: make(chan DatabaseOperation, 1000), // Buffer for operations
		workerDone:    make(chan struct{}),
	}

	// Start the serialization worker
	p.workerWg.Add(1)
	go p.serializationWorker()

	return p
}

// serializationWorker processes database operations one at a time
func (p *ProfiledEventStore) serializationWorker() {
	defer p.workerWg.Done()

	for {
		select {
		case op := <-p.operationChan:
			p.processOperation(op)
		case <-p.workerDone:
			// Process remaining operations before shutdown
			for {
				select {
				case op := <-p.operationChan:
					p.processOperation(op)
				default:
					return
				}
			}
		}
	}
}

// processOperation handles a single database operation
func (p *ProfiledEventStore) processOperation(op DatabaseOperation) {
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

	case OpQueryEvents:
		start := time.Now()
		ch, queryErr := p.backend.QueryEvents(context.Background(), *op.QueryFilter)
		duration := time.Since(start)

		p.mutex.Lock()
		p.stats.QueryEventsCalls++
		p.stats.QueryEventsDuration += duration
		p.mutex.Unlock()

		if duration > 500*time.Millisecond {
			log.Printf("🐌 SLOW QueryEvents: %v (filter: %+v)", duration, *op.QueryFilter)
		}

		if queryErr != nil {
			log.Printf("❌ QueryEvents error: %v", queryErr)
			closedCh := make(chan *nostr.Event)
			close(closedCh)
			result = closedCh
			err = queryErr
		} else {
			result = ch
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
	case p.operationChan <- op:
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

// QueryEvents profiles the QueryEvents method
func (p *ProfiledEventStore) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	resultChan := make(chan interface{}, 1)
	errorChan := make(chan error, 1)

	op := DatabaseOperation{
		Type:        OpQueryEvents,
		QueryFilter: &filter,
		ResultChan:  resultChan,
		ErrorChan:   errorChan,
	}

	select {
	case p.operationChan <- op:
		// Operation queued successfully
	case <-ctx.Done():
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, ctx.Err()
	}

	select {
	case result := <-resultChan:
		if ch, ok := result.(chan *nostr.Event); ok {
			return ch, nil
		}
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, nil
	case err := <-errorChan:
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, err
	case <-ctx.Done():
		closedCh := make(chan *nostr.Event)
		close(closedCh)
		return closedCh, ctx.Err()
	}
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
	case p.operationChan <- op:
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
	case p.operationChan <- op:
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
	case p.operationChan <- op:
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
	// Signal worker to stop
	close(p.workerDone)

	// Wait for worker to finish
	p.workerWg.Wait()

	// Close the operation channel
	close(p.operationChan)

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
		"operation_queue_size": len(p.operationChan),
		"operation_queue_cap":  cap(p.operationChan),
	})
}

// Stats returns database statistics from the underlying backend
func (p *ProfiledEventStore) Stats() interface{} {
	if sqliteBackend, ok := p.backend.(*sqlite3.SQLite3Backend); ok {
		return sqliteBackend.Stats()
	}
	return nil
}
