package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/policies"
	"github.com/girino/wot-relay/pkg/logger"
	"github.com/girino/wot-relay/pkg/profiling"
	"github.com/girino/wot-relay/pkg/sqlite"
	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
)

var (
	version string
)

// max returns the maximum of two int64 values
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Metrics tracks application metrics
type Metrics struct {
	StartTime           time.Time
	LastWoTRefresh      time.Time
	LastProfileRefresh  time.Time
	LastArchiving       time.Time
	TotalEvents         int64
	TrustedEvents       int64
	UntrustedEvents     int64
	NetworkSize         int64
	ActiveConnections   int64
	ProcessingQueueSize int64
	ErrorCount          int64
	LastError           time.Time
	LastErrorMsg        string
	mutex               sync.RWMutex
}

var metrics *Metrics

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		StartTime: time.Now(),
	}
}

// UpdateLastWoTRefresh updates the last WoT refresh time
func (m *Metrics) UpdateLastWoTRefresh() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.LastWoTRefresh = time.Now()
}

// UpdateLastProfileRefresh updates the last profile refresh time
func (m *Metrics) UpdateLastProfileRefresh() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.LastProfileRefresh = time.Now()
}

// UpdateLastArchiving updates the last archiving time
func (m *Metrics) UpdateLastArchiving() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.LastArchiving = time.Now()
}

// RecordError records an error
func (m *Metrics) RecordError(err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.ErrorCount++
	m.LastError = time.Now()
	m.LastErrorMsg = err.Error()
}

// UpdateNetworkSize updates the network size
func (m *Metrics) UpdateNetworkSize(size int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.NetworkSize = int64(size)
}

// UpdateProcessingQueueSize updates the processing queue size
func (m *Metrics) UpdateProcessingQueueSize(size int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.ProcessingQueueSize = int64(size)
}

// GetMetrics returns a copy of current metrics
func (m *Metrics) GetMetrics() Metrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return Metrics{
		StartTime:           m.StartTime,
		LastWoTRefresh:      m.LastWoTRefresh,
		LastProfileRefresh:  m.LastProfileRefresh,
		LastArchiving:       m.LastArchiving,
		TotalEvents:         atomic.LoadInt64(&m.TotalEvents),
		TrustedEvents:       atomic.LoadInt64(&m.TrustedEvents),
		UntrustedEvents:     atomic.LoadInt64(&m.UntrustedEvents),
		NetworkSize:         m.NetworkSize,
		ActiveConnections:   atomic.LoadInt64(&m.ActiveConnections),
		ProcessingQueueSize: m.ProcessingQueueSize,
		ErrorCount:          atomic.LoadInt64(&m.ErrorCount),
		LastError:           m.LastError,
		LastErrorMsg:        m.LastErrorMsg,
	}
}

type Config struct {
	RelayName        string
	RelayPubkey      string
	RelayDescription string
	DBPath           string
	RelayURL         string
	IndexPath        string
	StaticPath       string
	RefreshInterval  int
	MinimumFollowers int
	ArchivalSync     bool
	RelayContact     string
	RelayIcon        string
	MaxAgeDays       int
	ArchiveReactions bool
	ArchiveMaxDays   int
	IgnoredPubkeys   []string
	WoTDepth         int
}

// Remove persistent pool - we'll create connections on-demand
var wdb nostr.RelayStore
var config Config

// createTemporaryPool creates a temporary pool for a specific operation
func createTemporaryPool(ctx context.Context) *nostr.SimplePool {
	logger.Debug("RELAY", "Creating temporary relay pool", map[string]interface{}{
		"seed_relays": len(seedRelays),
	})
	return nostr.NewSimplePool(ctx)
}

// var trustNetwork []string
var seedRelays []string
var trustNetworkMap map[string]bool
var trustNetworkMutex sync.RWMutex
var trustedNotes int64
var untrustedNotes int64
var indexTemplate *template.Template
var eventProcessor *EventProcessor
var memoryMonitor *MemoryMonitor

// Memory monitoring
type MemoryMonitor struct {
	maxMemoryMB      int64
	warningThreshold float64
}

func NewMemoryMonitor(maxMemoryMB int64) *MemoryMonitor {
	return &MemoryMonitor{
		maxMemoryMB:      maxMemoryMB,
		warningThreshold: 0.8, // Warn at 80% of max memory
	}
}

func (mm *MemoryMonitor) CheckMemory() (bool, string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	currentMB := int64(m.Alloc / 1024 / 1024)

	if currentMB > mm.maxMemoryMB {
		return true, fmt.Sprintf("Memory limit exceeded: %dMB > %dMB", currentMB, mm.maxMemoryMB)
	}

	if float64(currentMB)/float64(mm.maxMemoryMB) > mm.warningThreshold {
		return false, fmt.Sprintf("Memory warning: %dMB (%.1f%% of limit)", currentMB, float64(currentMB)/float64(mm.maxMemoryMB)*100)
	}

	return false, ""
}

// Worker pool for event processing
type EventProcessor struct {
	workerCount int
	eventChan   chan nostr.Event
	quit        chan struct{}
	wg          sync.WaitGroup
	busyWorkers int64 // Atomic counter for busy workers
}

func NewEventProcessor(workerCount int) *EventProcessor {
	// Get worker multiplier from environment, default to 50
	workerMultiplier := 50 // Default multiplier
	if multiplierEnv := os.Getenv("WORKER_MULTIPLIER"); multiplierEnv != "" {
		if parsed, err := strconv.Atoi(multiplierEnv); err == nil && parsed > 0 {
			workerMultiplier = parsed
		}
	}

	return &EventProcessor{
		workerCount: workerCount,
		eventChan:   make(chan nostr.Event, workerCount*workerMultiplier), // Buffer for Nx worker count
		quit:        make(chan struct{}),
	}
}

func (ep *EventProcessor) Start(ctx context.Context, relay *khatru.Relay) {
	for i := 0; i < ep.workerCount; i++ {
		ep.wg.Add(1)
		go ep.worker(ctx, relay)
	}
}

func (ep *EventProcessor) worker(ctx context.Context, relay *khatru.Relay) {
	defer ep.wg.Done()

	for {
		select {
		case event, ok := <-ep.eventChan:
			if !ok {
				// Channel is closed, exit gracefully
				return
			}
			// Mark worker as busy
			atomic.AddInt64(&ep.busyWorkers, 1)
			archiveEvent(ctx, relay, event)
			// Mark worker as idle
			atomic.AddInt64(&ep.busyWorkers, -1)
		case <-ep.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (ep *EventProcessor) QueueEvent(event nostr.Event) {
	// Block until we can send the event - don't drop any events
	// Check if channel is closed to avoid panic
	select {
	case ep.eventChan <- event:
		// Event queued successfully
	case <-ep.quit:
		// Processor is shutting down, drop the event
		return
	}
}

func (ep *EventProcessor) Stop() {
	close(ep.quit)
	close(ep.eventChan) // Close the event channel to unblock workers
	ep.wg.Wait()
}

func (ep *EventProcessor) GetWorkerStats() map[string]interface{} {
	busyWorkers := atomic.LoadInt64(&ep.busyWorkers)
	return map[string]interface{}{
		"total_workers":  ep.workerCount,
		"busy_workers":   busyWorkers,
		"idle_workers":   ep.workerCount - int(busyWorkers),
		"queue_size":     len(ep.eventChan),
		"queue_capacity": cap(ep.eventChan),
	}
}

func main() {
	nostr.InfoLogger = log.New(io.Discard, "", 0)
	green := "\033[32m"
	reset := "\033[0m"

	art := `
888       888      88888888888      8888888b.          888                   
888   o   888          888          888   Y88b         888                   
888  d8b  888          888          888    888         888                   
888 d888b 888  .d88b.  888          888   d88P .d88b.  888  8888b.  888  888 
888d88888b888 d88""88b 888          8888888P" d8P  Y8b 888     "88b 888  888 
88888P Y88888 888  888 888          888 T88b  88888888 888 .d888888 888  888 
8888P   Y8888 Y88..88P 888          888  T88b Y8b.     888 888  888 Y88b 888 
888P     Y888  "Y88P"  888          888   T88b "Y8888  888 "Y888888  "Y88888 
                                                                         888 
                                                                    Y8b d88P 
                                               powered by: khatru     "Y88P"  
	`

	fmt.Println(green + art + reset)

	// Initialize logger and metrics
	var logLevel logger.LogLevel = logger.INFO
	if os.Getenv("LOG_LEVEL") == "DEBUG" {
		logLevel = logger.DEBUG
	}
	logger.SetDefault(logger.NewLogger(logLevel))
	metrics = NewMetrics()

	logger.Info("MAIN", "Booting up web of trust relay")
	relay := khatru.NewRelay()
	ctx, cancel := context.WithCancel(context.Background())
	config = LoadConfig()

	// Initialize template once at startup
	indexTemplate = template.Must(template.ParseFiles(config.IndexPath))

	relay.Info.Name = config.RelayName
	relay.Info.PubKey = config.RelayPubkey
	relay.Info.Icon = config.RelayIcon
	relay.Info.Contact = config.RelayContact
	relay.Info.Description = config.RelayDescription
	relay.Info.Software = "https://github.com/girino/wot-relay"
	relay.Info.Version = version

	// Create optimized SQLite backend (implements eventstore.Store interface)
	logger.Info("MAIN", "Creating SQLite backend", map[string]interface{}{"db_path": config.DBPath})
	sqliteBackend := sqlite.NewOptimizedSQLiteBackend(config.DBPath)

	// Create profiled event store wrapper (adds profiling to the backend)
	db := profiling.NewProfiledEventStore(sqliteBackend)
	if err := db.Init(); err != nil {
		panic(err)
	}

	wdb = eventstore.RelayWrapper{Store: db}

	relay.RejectEvent = append(relay.RejectEvent,
		policies.RejectEventsWithBase64Media,
		policies.EventIPRateLimiter(50, time.Minute*1, 300),
	)

	// Temporarily disabled to test if filters are blocking queries
	//relay.RejectFilter = append(relay.RejectFilter,
	//	policies.NoEmptyFilters,
	//	policies.NoComplexFilters,
	//)

	relay.RejectConnection = append(relay.RejectConnection,
		policies.ConnectionRateLimiter(10, time.Minute*2, 30),
	)

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, db.QueryEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.RejectEvent = append(relay.RejectEvent, func(ctx context.Context, event *nostr.Event) (bool, string) {
		// Don't reject events if we haven't booted yet or if trust network is empty
		trustNetworkMutex.RLock()
		hasNetwork := len(trustNetworkMap) > 1
		trusted := trustNetworkMap[event.PubKey]
		trustNetworkMutex.RUnlock()

		// If we don't have a trust network yet, allow all events
		if !hasNetwork {
			return false, ""
		}

		if !trusted {
			return true, "not in web of trust"
		}
		if event.Kind == nostr.KindEncryptedDirectMessage {
			return true, "only gift wrapped DMs are allowed"
		}

		return false, ""
	})

	// Initialize seed relays from environment variable
	seedRelaysEnv := getEnv("SEED_RELAYS")
	if seedRelaysEnv == "" {
		// Default seed relays if not configured
		seedRelays = []string{
			"wss://relay.primal.net",
			"wss://relay.damus.io",
			"wss://nos.lol",
			"wss://wot.utxo.one/",
			"wss://nostr.mom",
		}
	} else {
		// Parse comma-separated relay URLs from environment
		seedRelays = strings.Split(seedRelaysEnv, ",")
		// Trim whitespace from each relay URL
		for i, relay := range seedRelays {
			seedRelays[i] = strings.TrimSpace(relay)
		}
	}

	// Initialize performance components
	// Get worker count from environment, default to 2 per processor
	workerCount := runtime.NumCPU() * 2 // Default to 2 workers per CPU core
	if workerCountEnv := os.Getenv("WORKER_COUNT"); workerCountEnv != "" {
		if parsed, err := strconv.Atoi(workerCountEnv); err == nil && parsed > 0 {
			workerCount = parsed
		}
	}
	eventProcessor = NewEventProcessor(workerCount)
	eventProcessor.Start(ctx, relay)

	memoryMonitor = NewMemoryMonitor(1024) // 1GB memory limit

	go refreshTrustNetwork(ctx, relay)
	go monitorResources()

	mux := relay.Router()
	static := http.FileServer(http.Dir(config.StaticPath))

	mux.Handle("GET /static/", http.StripPrefix("/static/", static))
	mux.Handle("GET /favicon.ico", http.StripPrefix("/", static))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			RelayName        string
			RelayPubkey      string
			RelayDescription string
			RelayURL         string
		}{
			RelayName:        config.RelayName,
			RelayPubkey:      config.RelayPubkey,
			RelayDescription: config.RelayDescription,
			RelayURL:         config.RelayURL,
		}
		err := indexTemplate.Execute(w, data)
		if err != nil {
			logger.Error("WEB", "Template execution error", map[string]interface{}{"error": err.Error()})
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		health := map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
			"uptime":    time.Since(metrics.StartTime).Seconds(),
		}

		// Check if any critical processes are stuck
		now := time.Now()
		if !metrics.LastWoTRefresh.IsZero() && now.Sub(metrics.LastWoTRefresh) > time.Duration(config.RefreshInterval+1)*time.Hour {
			health["status"] = "unhealthy"
			health["issues"] = []string{"WoT refresh is overdue"}
		}

		if !metrics.LastProfileRefresh.IsZero() && now.Sub(metrics.LastProfileRefresh) > time.Duration(config.RefreshInterval+1)*time.Hour {
			health["status"] = "unhealthy"
			if issues, ok := health["issues"].([]string); ok {
				health["issues"] = append(issues, "Profile refresh is overdue")
			} else {
				health["issues"] = []string{"Profile refresh is overdue"}
			}
		}

		// Check processing queue
		if metrics.ProcessingQueueSize > 10000 {
			health["status"] = "degraded"
			if issues, ok := health["issues"].([]string); ok {
				health["issues"] = append(issues, "Processing queue is overloaded")
			} else {
				health["issues"] = []string{"Processing queue is overloaded"}
			}
		}

		statusCode := http.StatusOK
		if health["status"] == "unhealthy" {
			statusCode = http.StatusServiceUnavailable
		} else if health["status"] == "degraded" {
			statusCode = http.StatusOK // Still OK but with warnings
		}

		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(health)
	})

	// Stats endpoint
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		currentMetrics := metrics.GetMetrics()

		// Get runtime stats
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		// Get goroutine count
		goroutines := runtime.NumGoroutine()

		// Warn if goroutine count is high
		if goroutines > 100 {
			logger.Warn("SYSTEM", "High goroutine count detected", map[string]interface{}{
				"goroutines": goroutines,
				"threshold":  100,
			})
		}

		// Get database stats if available
		var dbStats map[string]interface{}
		var eventStoreStats map[string]interface{}
		logger.Info("STATS", "Starting database stats collection", map[string]interface{}{
			"wdb_nil": wdb == nil,
		})
		if wdb != nil {
			logger.Info("STATS", "wdb is not nil, attempting type assertion", map[string]interface{}{
				"wdb_type": fmt.Sprintf("%T", wdb),
			})
			if db, ok := wdb.(eventstore.RelayWrapper); ok {
				logger.Info("STATS", "Successfully cast to RelayWrapper", map[string]interface{}{
					"store_nil": db.Store == nil,
				})
				// Debug: log the actual type of db.Store
				logger.Info("STATS", "Database store type", map[string]interface{}{
					"store_type": fmt.Sprintf("%T", db.Store),
				})
				if profiledDB, ok := db.Store.(*profiling.ProfiledEventStore); ok {
					// Get eventstore performance stats
					perfStats := profiledDB.GetStats()
					eventStoreStats = map[string]interface{}{
						// Total time (including semaphore wait time)
						"save_event_calls":     perfStats.SaveEventCalls,
						"save_event_avg_ms":    perfStats.SaveEventDuration.Milliseconds() / max(1, perfStats.SaveEventCalls),
						"query_events_calls":   perfStats.QueryEventsCalls,
						"query_events_avg_ms":  perfStats.QueryEventsDuration.Milliseconds() / max(1, perfStats.QueryEventsCalls),
						"delete_event_calls":   perfStats.DeleteEventCalls,
						"delete_event_avg_ms":  perfStats.DeleteEventDuration.Milliseconds() / max(1, perfStats.DeleteEventCalls),
						"replace_event_calls":  perfStats.ReplaceEventCalls,
						"replace_event_avg_ms": perfStats.ReplaceEventDuration.Milliseconds() / max(1, perfStats.ReplaceEventCalls),

						// Pure database time (excluding semaphore wait time)
						"save_event_db_avg_ms":    perfStats.SaveEventDBDuration.Milliseconds() / max(1, perfStats.SaveEventCalls),
						"query_events_db_avg_ms":  perfStats.QueryEventsDBDuration.Milliseconds() / max(1, perfStats.QueryEventsCalls),
						"delete_event_db_avg_ms":  perfStats.DeleteEventDBDuration.Milliseconds() / max(1, perfStats.DeleteEventCalls),
						"replace_event_db_avg_ms": perfStats.ReplaceEventDBDuration.Milliseconds() / max(1, perfStats.ReplaceEventCalls),

						// Semaphore usage
						"write_semaphore_used": len(profiledDB.WriteSemaphore),
						"write_semaphore_cap":  cap(profiledDB.WriteSemaphore),
						"read_semaphore_used":  len(profiledDB.ReadSemaphore),
						"read_semaphore_cap":   cap(profiledDB.ReadSemaphore),
					}
					logger.Info("STATS", "Profiled database stats found", map[string]interface{}{
						"save_calls":   perfStats.SaveEventCalls,
						"query_calls":  perfStats.QueryEventsCalls,
						"delete_calls": perfStats.DeleteEventCalls,
					})
					// Also get SQLite connection stats
					if sqliteDB, ok := profiledDB.GetBackend().(*sqlite3.SQLite3Backend); ok {
						stats := sqliteDB.Stats()
						dbStats = map[string]interface{}{
							"open_connections": stats.OpenConnections,
							"in_use":           stats.InUse,
							"idle":             stats.Idle,
							"wait_count":       stats.WaitCount,
						}
					}
				} else {
					logger.Info("STATS", "Type assertion failed - not ProfiledEventStore", map[string]interface{}{
						"actual_type": fmt.Sprintf("%T", db.Store),
					})
				}
			} else {
				logger.Info("STATS", "Failed to cast wdb to RelayWrapper", map[string]interface{}{
					"wdb_type": fmt.Sprintf("%T", wdb),
				})
			}
		} else {
			logger.Info("STATS", "wdb is nil - no database stats available")
		}

		// Get worker stats if available
		var workerStats map[string]interface{}
		if eventProcessor != nil {
			workerStats = eventProcessor.GetWorkerStats()
		}

		stats := map[string]interface{}{
			"relay": map[string]interface{}{
				"name":        config.RelayName,
				"pubkey":      config.RelayPubkey,
				"description": config.RelayDescription,
				"version":     version,
			},
			"uptime": map[string]interface{}{
				"start_time": currentMetrics.StartTime,
				"duration":   time.Since(currentMetrics.StartTime).Seconds(),
			},
			"network": map[string]interface{}{
				"size":                 currentMetrics.NetworkSize,
				"last_wot_refresh":     currentMetrics.LastWoTRefresh,
				"last_profile_refresh": currentMetrics.LastProfileRefresh,
				"last_archiving":       currentMetrics.LastArchiving,
			},
			"events": map[string]interface{}{
				"total":            currentMetrics.TotalEvents,
				"trusted":          currentMetrics.TrustedEvents,
				"untrusted":        currentMetrics.UntrustedEvents,
				"processing_queue": currentMetrics.ProcessingQueueSize,
			},
			"system": map[string]interface{}{
				"goroutines":         goroutines,
				"memory_mb":          m.Alloc / 1024 / 1024,
				"gc_runs":            m.NumGC,
				"active_connections": currentMetrics.ActiveConnections,
			},
			"errors": map[string]interface{}{
				"count":          currentMetrics.ErrorCount,
				"last_error":     currentMetrics.LastError,
				"last_error_msg": currentMetrics.LastErrorMsg,
			},
		}

		if workerStats != nil {
			stats["workers"] = workerStats
		}

		if dbStats != nil {
			stats["database"] = dbStats
		}

		if eventStoreStats != nil {
			stats["eventstore"] = eventStoreStats
		}

		json.NewEncoder(w).Encode(stats)
	})

	// Create server with graceful shutdown
	server := &http.Server{
		Addr:    ":3334",
		Handler: relay,
	}

	go func() {
		logger.Info("SERVER", "Relay server started", map[string]interface{}{
			"port":    ":3334",
			"version": version,
		})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("SERVER", "Server failed to start", map[string]interface{}{
				"error": err.Error(),
			})
			log.Fatal(err)
		}
	}()

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info("SHUTDOWN", "Initiating graceful shutdown")

	// Cancel the main context to stop all background processes
	cancel()

	// Give background processes a moment to finish gracefully
	logger.Info("SHUTDOWN", "Waiting for background processes to finish")
	time.Sleep(2 * time.Second)

	// Stop processors first
	if eventProcessor != nil {
		logger.Info("SHUTDOWN", "Stopping event processor")
		eventProcessor.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("SHUTDOWN", "Server shutdown error", map[string]interface{}{
			"error": err.Error(),
		})
	}
	logger.Info("SHUTDOWN", "Server stopped successfully")
}

func monitorResources() {
	for {
		func() {
			var m runtime.MemStats
			goroutines := runtime.NumGoroutine()
			runtime.ReadMemStats(&m)

			trustNetworkMutex.RLock()
			networkSize := len(trustNetworkMap)
			trustNetworkMutex.RUnlock()

			// Update processing queue size
			queueSize := len(eventProcessor.eventChan)
			metrics.UpdateProcessingQueueSize(queueSize)

			logger.Info("MONITOR", "System status", map[string]interface{}{
				"goroutines":            goroutines,
				"memory_alloc_mb":       m.Alloc / 1024 / 1024,
				"memory_sys_mb":         m.Sys / 1024 / 1024,
				"gc_runs":               m.NumGC,
				"network_size":          networkSize,
				"trusted_notes":         atomic.LoadInt64(&trustedNotes),
				"untrusted_notes":       atomic.LoadInt64(&untrustedNotes),
				"processing_queue_size": queueSize,
			})

			// Database performance monitoring
			if wdb != nil {
				if db, ok := wdb.(eventstore.RelayWrapper); ok {
					if profiledDB, ok := db.Store.(*profiling.ProfiledEventStore); ok {
						// Log eventstore performance stats
						profiledDB.LogStats(logger.GetDefault())

						// Also get SQLite connection stats
						if sqliteDB, ok := profiledDB.GetBackend().(*sqlite3.SQLite3Backend); ok {
							stats := sqliteDB.Stats()
							logger.Info("MONITOR", "Database connections", map[string]interface{}{
								"open_connections": stats.OpenConnections,
								"in_use":           stats.InUse,
								"idle":             stats.Idle,
								"wait_count":       stats.WaitCount,
							})
						}
					}
				}
			}

			// Memory monitoring
			if memoryMonitor != nil {
				exceeded, message := memoryMonitor.CheckMemory()
				if exceeded {
					logger.Error("MONITOR", "Memory limit exceeded", map[string]interface{}{
						"message": message,
					})
					// Force garbage collection when memory limit exceeded
					runtime.GC()
				} else if message != "" {
					logger.Warn("MONITOR", "Memory warning", map[string]interface{}{
						"message": message,
					})
				}
			}
		}()
		time.Sleep(300 * time.Second)
	}
}

func LoadConfig() Config {
	godotenv.Load(".env")

	if os.Getenv("REFRESH_INTERVAL_HOURS") == "" {
		os.Setenv("REFRESH_INTERVAL_HOURS", "3")
	}

	refreshInterval, _ := strconv.Atoi(os.Getenv("REFRESH_INTERVAL_HOURS"))
	logger.Info("CONFIG", "Refresh interval configured", map[string]interface{}{"hours": refreshInterval})

	if os.Getenv("MINIMUM_FOLLOWERS") == "" {
		os.Setenv("MINIMUM_FOLLOWERS", "1")
	}

	if os.Getenv("ARCHIVAL_SYNC") == "" {
		os.Setenv("ARCHIVAL_SYNC", "TRUE")
	}

	if os.Getenv("RELAY_ICON") == "" {
		os.Setenv("RELAY_ICON", "https://pfp.nostr.build/56306a93a88d4c657d8a3dfa57b55a4ed65b709eee927b5dafaab4d5330db21f.png")
	}

	if os.Getenv("RELAY_CONTACT") == "" {
		os.Setenv("RELAY_CONTACT", getEnv("RELAY_PUBKEY"))
	}

	if os.Getenv("MAX_AGE_DAYS") == "" {
		os.Setenv("MAX_AGE_DAYS", "0")
	}

	if os.Getenv("ARCHIVE_REACTIONS") == "" {
		os.Setenv("ARCHIVE_REACTIONS", "FALSE")
	}

	if os.Getenv("MAX_TRUST_NETWORK") == "" {
		os.Setenv("MAX_TRUST_NETWORK", "40000")
	}

	if os.Getenv("MAX_RELAYS") == "" {
		os.Setenv("MAX_RELAYS", "1000")
	}

	if os.Getenv("MAX_ONE_HOP_NETWORK") == "" {
		os.Setenv("MAX_ONE_HOP_NETWORK", "50000")
	}

	ignoredPubkeys := []string{}
	if ignoreList := os.Getenv("IGNORE_FOLLOWS_LIST"); ignoreList != "" {
		ignoredPubkeys = splitAndTrim(ignoreList)
	}

	if os.Getenv("WOT_DEPTH") == "" {
		os.Setenv("WOT_DEPTH", "2")
	}

	if os.Getenv("ARCHIVE_MAX_DAYS") == "" {
		os.Setenv("ARCHIVE_MAX_DAYS", "15")
	}

	if os.Getenv("LOG_LEVEL") == "" {
		os.Setenv("LOG_LEVEL", "INFO")
	}

	minimumFollowers, _ := strconv.Atoi(os.Getenv("MINIMUM_FOLLOWERS"))
	maxAgeDays, _ := strconv.Atoi(os.Getenv("MAX_AGE_DAYS"))
	archiveMaxDays, _ := strconv.Atoi(os.Getenv("ARCHIVE_MAX_DAYS"))
	woTDepth, _ := strconv.Atoi(os.Getenv("WOT_DEPTH"))

	config := Config{
		RelayName:        getEnv("RELAY_NAME"),
		RelayPubkey:      getEnv("RELAY_PUBKEY"),
		RelayDescription: getEnv("RELAY_DESCRIPTION"),
		RelayContact:     getEnv("RELAY_CONTACT"),
		RelayIcon:        getEnv("RELAY_ICON"),
		DBPath:           getEnv("DB_PATH"),
		RelayURL:         getEnv("RELAY_URL"),
		IndexPath:        getEnv("INDEX_PATH"),
		StaticPath:       getEnv("STATIC_PATH"),
		RefreshInterval:  refreshInterval,
		MinimumFollowers: minimumFollowers,
		ArchivalSync:     getEnv("ARCHIVAL_SYNC") == "TRUE",
		MaxAgeDays:       maxAgeDays,
		ArchiveReactions: getEnv("ARCHIVE_REACTIONS") == "TRUE",
		ArchiveMaxDays:   archiveMaxDays,
		IgnoredPubkeys:   ignoredPubkeys,
		WoTDepth:         woTDepth,
	}

	return config
}

func getEnv(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatalf("Environment variable %s not set", key)
	}
	return value
}

func updateTrustNetworkFilter(pubkeyFollowerCount map[string]int) {
	myTrustNetworkMap := make(map[string]bool)

	logger.Info("WOT", "Building new trust network map")

	for pubkey, count := range pubkeyFollowerCount {
		if count >= config.MinimumFollowers {
			myTrustNetworkMap[pubkey] = true
		}
	}

	// Thread-safe update of trust network map
	trustNetworkMutex.Lock()
	trustNetworkMap = myTrustNetworkMap
	trustNetworkMutex.Unlock()

	logger.Info("WOT", "Trust network map updated", map[string]interface{}{"key_count": len(myTrustNetworkMap)})
}

func refreshProfiles(ctx context.Context) {
	logger.Info("PROFILES", "Starting profile refresh")
	startTime := time.Now()

	trustNetworkMutex.RLock()
	trustNetwork := make([]string, 0, len(trustNetworkMap))
	for pubkey := range trustNetworkMap {
		trustNetwork = append(trustNetwork, pubkey)
	}
	trustNetworkMutex.RUnlock()

	// Find pubkeys that need profile refresh (missing profiles only) using batch queries
	pubkeysToRefresh := make([]string, 0)
	existingProfiles := make(map[string]bool)

	logger.Info("PROFILES", "Checking profiles for refresh", map[string]interface{}{
		"total_profiles": len(trustNetwork),
		"check_type":     "missing_only",
		"batch_size":     1000,
	})

	// Process in batches to avoid "too many authors" errors
	batchSize := 1000
	for i := 0; i < len(trustNetwork); i += batchSize {
		end := i + batchSize
		if end > len(trustNetwork) {
			end = len(trustNetwork)
		}

		batch := trustNetwork[i:end]

		// Query for existing profiles in this batch
		filter := nostr.Filter{
			Authors: batch,
			Kinds:   []int{nostr.KindProfileMetadata},
			Limit:   1,
		}

		// Quick check with reasonable timeout
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		ch, err := wdb.QueryEvents(checkCtx, filter)
		if err == nil {
			for ev := range ch {
				existingProfiles[ev.PubKey] = true
			}
		}
		cancel()

		// Log progress every batch
		progress := float64(end) / float64(len(trustNetwork)) * 100
		logger.Info("PROFILES", "Profile check progress", map[string]interface{}{
			"checked":      end,
			"total":        len(trustNetwork),
			"progress_pct": progress,
			"batch_size":   len(batch),
		})
	}

	// Find missing profiles
	for _, pubkey := range trustNetwork {
		if !existingProfiles[pubkey] {
			pubkeysToRefresh = append(pubkeysToRefresh, pubkey)
		}
	}

	logger.Info("PROFILES", "Profile refresh analysis complete", map[string]interface{}{
		"profiles_to_refresh": len(pubkeysToRefresh),
		"total_profiles":      len(trustNetwork),
		"refresh_percentage":  float64(len(pubkeysToRefresh)) / float64(len(trustNetwork)) * 100,
	})

	if len(pubkeysToRefresh) == 0 {
		logger.Info("PROFILES", "All profiles are up to date")
		metrics.UpdateLastProfileRefresh()
		return
	}

	// Refresh only the profiles that need updating
	stepSize := 200
	for i := 0; i < len(pubkeysToRefresh); i += stepSize {
		logger.Info("PROFILES", "Refreshing profile batch", map[string]interface{}{
			"batch_start":         i,
			"batch_end":           i + stepSize,
			"total_profiles":      len(pubkeysToRefresh),
			"progress_percentage": float64(i) / float64(len(pubkeysToRefresh)) * 100,
		})

		// force cancel context every time
		func() {
			timeout, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			end := i + stepSize
			if end > len(pubkeysToRefresh) {
				end = len(pubkeysToRefresh)
			}

			filters := []nostr.Filter{{
				Authors: pubkeysToRefresh[i:end],
				Kinds:   []int{nostr.KindProfileMetadata},
			}}

			// Create temporary pool for this operation
			tempPool := createTemporaryPool(ctx)

			for ev := range tempPool.SubManyEose(timeout, seedRelays, filters) {
				wdb.Publish(ctx, *ev.Event)
			}
		}()
	}

	duration := time.Since(startTime)
	logger.Info("PROFILES", "Profile refresh completed", map[string]interface{}{
		"profiles_refreshed": len(pubkeysToRefresh),
		"total_profiles":     len(trustNetwork),
		"duration_seconds":   duration.Seconds(),
	})
	metrics.UpdateLastProfileRefresh()
}

func refreshTrustNetwork(ctx context.Context, relay *khatru.Relay) {

	runTrustNetworkRefresh := func(wotDepth int) map[string]int {
		newPubkeyFollowerCount := make(map[string]int)
		lastPubkeyFollowerCount := make(map[string]int)
		// initializes with seed pubkey
		newPubkeyFollowerCount[config.RelayPubkey]++

		logger.Info("WOT", "Building web of trust graph", map[string]interface{}{"depth": wotDepth})
		for j := 0; j < wotDepth; j++ {
			logger.Info("WOT", "Processing WoT depth", map[string]interface{}{"depth": j, "total_depth": wotDepth})
			oneHopNetwork := make([]string, 0)
			for pubkey := range newPubkeyFollowerCount {
				if _, exists := lastPubkeyFollowerCount[pubkey]; !exists {
					if isValidPubkey(pubkey) {
						oneHopNetwork = append(oneHopNetwork, pubkey)
					} else {
						logger.Warn("WOT", "Invalid pubkey in one hop network", map[string]interface{}{"pubkey": pubkey})
					}
				}
			}
			lastPubkeyFollowerCount = make(map[string]int)
			for pubkey, count := range newPubkeyFollowerCount {
				lastPubkeyFollowerCount[pubkey] = count
			}

			stepSize := 300
			for i := 0; i < len(oneHopNetwork); i += stepSize {
				logger.Info("WOT", "Fetching followers batch", map[string]interface{}{
					"batch_start":   i,
					"batch_end":     i + stepSize,
					"total_network": len(oneHopNetwork),
					"depth":         j,
				})

				end := i + stepSize
				if end > len(oneHopNetwork) {
					end = len(oneHopNetwork)
				}

				filters := []nostr.Filter{{
					Authors: oneHopNetwork[i:end],
					Kinds:   []int{nostr.KindFollowList, nostr.KindRelayListMetadata, nostr.KindProfileMetadata},
				}}

				func() { // avoid "too many concurrent reqs" error
					timeout, cancel := context.WithTimeout(ctx, 2*time.Minute)
					defer cancel()
					// Create temporary pool for this operation
					tempPool := createTemporaryPool(ctx)

					for ev := range tempPool.SubManyEose(timeout, seedRelays, filters) {
						for _, contact := range ev.Event.Tags {
							if len(contact) > 0 && contact[0] == "p" {
								if len(contact) > 1 && len(contact[1]) == 64 {
									pubkey := contact[1]
									if isIgnored(pubkey, config.IgnoredPubkeys) {
										fmt.Println("ignoring follows from pubkey: ", pubkey)
										continue
									}
									if !isValidPubkey(pubkey) {
										fmt.Println("invalid pubkey in follows: ", pubkey)
										continue
									}
									newPubkeyFollowerCount[pubkey]++ // Increment follower count for the pubkey
								}
							}
						}

						if ev.Event.Kind == nostr.KindProfileMetadata {
							wdb.Publish(ctx, *ev.Event)
						}
					}
				}()
			}
		}
		logger.Info("WOT", "Web of trust graph completed", map[string]interface{}{
			"total_network_size": len(newPubkeyFollowerCount),
			"depth":              wotDepth,
		})
		metrics.UpdateLastWoTRefresh()
		metrics.UpdateNetworkSize(len(newPubkeyFollowerCount))
		return newPubkeyFollowerCount
	}

	// build partial trust network if woTDepth is > 2 and its first run
	if config.WoTDepth > 2 && len(trustNetworkMap) <= 1 {

		updateTrustNetworkFilter(runTrustNetworkRefresh(2))
	}

	archivingCompleted := false

	for {
		cycleStart := time.Now()

		// WoT refresh with timeout detection
		wotCtx, wotCancel := context.WithTimeout(ctx, time.Duration(config.RefreshInterval)*time.Hour/2)
		func() {
			defer wotCancel()
			updateTrustNetworkFilter(runTrustNetworkRefresh(config.WoTDepth))
		}()

		// Check if WoT refresh timed out
		if wotCtx.Err() == context.DeadlineExceeded {
			logger.Error("MAIN", "WoT refresh timed out", map[string]interface{}{
				"timeout_minutes": config.RefreshInterval * 30,
			})
			metrics.RecordError(fmt.Errorf("WoT refresh timed out"))
		}

		// Profile refresh with timeout detection
		profileCtx, profileCancel := context.WithTimeout(ctx, 30*time.Minute)
		func() {
			defer profileCancel()
			refreshProfiles(profileCtx)
		}()

		// Check if profile refresh timed out
		if profileCtx.Err() == context.DeadlineExceeded {
			logger.Error("MAIN", "Profile refresh timed out", map[string]interface{}{
				"timeout_minutes": 30,
			})
			metrics.RecordError(fmt.Errorf("Profile refresh timed out"))
		}

		// Cleanup old notes
		deleteOldNotes(relay)

		// Run archiving only once, after the first full WoT network is built and profiles are refreshed
		if !archivingCompleted {
			archiveCtx, archiveCancel := context.WithTimeout(ctx, 2*time.Hour)
			func() {
				defer archiveCancel()
				archiveTrustedNotes(archiveCtx, relay)
			}()

			// Check if archiving timed out
			if archiveCtx.Err() == context.DeadlineExceeded {
				logger.Error("MAIN", "Archiving timed out", map[string]interface{}{
					"timeout_minutes": 120,
				})
				metrics.RecordError(fmt.Errorf("Archiving timed out"))
			}

			archivingCompleted = true
		}

		cycleDuration := time.Since(cycleStart)
		logger.Info("MAIN", "Web of trust refresh cycle completed", map[string]interface{}{
			"next_refresh_hours":     config.RefreshInterval,
			"cycle_duration_minutes": cycleDuration.Minutes(),
		})

		// Reset performance stats for clean metrics in next cycle
		if wdb != nil {
			if db, ok := wdb.(eventstore.RelayWrapper); ok {
				if profiledDB, ok := db.Store.(*profiling.ProfiledEventStore); ok {
					profiledDB.ResetStats()
				}
			}
		}

		// Wait for the configured refresh interval before next cycle
		time.Sleep(time.Duration(config.RefreshInterval) * time.Hour)
	}
}

func archiveTrustedNotes(ctx context.Context, relay *khatru.Relay) {
	timeout, cancel := context.WithTimeout(ctx, time.Duration(config.RefreshInterval)*time.Hour)
	defer cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)
		if config.ArchivalSync {
			// Run archiving first, then profile refresh sequentially
			logger.Info("ARCHIVE", "Starting archiving process", map[string]interface{}{
				"max_days": config.ArchiveMaxDays,
			})

			var filters []nostr.Filter
			if config.ArchiveReactions {
				filters = []nostr.Filter{{
					Kinds: []int{
						nostr.KindTextNote,
						nostr.KindArticle,
						nostr.KindDeletion,
						nostr.KindEncryptedDirectMessage,
						nostr.KindMuteList,
						nostr.KindReaction,
						nostr.KindRepost,
						nostr.KindZapRequest,
						nostr.KindZap,
						nostr.KindProfileMetadata,
						nostr.KindRelayListMetadata,
						nostr.KindFollowList,
					},
				}}
			} else {
				filters = []nostr.Filter{{
					Kinds: []int{
						nostr.KindTextNote,
						nostr.KindArticle,
						nostr.KindDeletion,
						nostr.KindEncryptedDirectMessage,
						nostr.KindMuteList,
						nostr.KindRepost,
						nostr.KindZapRequest,
						nostr.KindZap,
						nostr.KindProfileMetadata,
						nostr.KindRelayListMetadata,
						nostr.KindFollowList,
					},
				}}
			}

			logger.Info("ARCHIVE", "Archiving trusted notes")

			// Paginate through historical events (configurable max days)
			archiveMaxDays := config.ArchiveMaxDays
			if archiveMaxDays <= 0 {
				archiveMaxDays = 15 // Default to 15 days
			}
			maxArchiveTime := nostr.Now() - (nostr.Timestamp(archiveMaxDays) * 24 * 60 * 60)

			// Process each event kind separately for better pagination control
			totalEvents := 0
			seenEvents := make(map[string]bool, 1000) // Global deduplication cache

			for _, kind := range filters[0].Kinds {
				logger.Info("ARCHIVE", "Processing events", map[string]interface{}{"kind": kind})

				// Use nak-style pagination for this specific kind
				until := nostr.Now()
				limit := 500 // Reasonable limit per request
				kindEvents := 0
				pageCount := 0

				for {
					pageCount++
					logger.Info("ARCHIVE", "Processing page", map[string]interface{}{"kind": kind, "page": pageCount, "until": until, "limit": limit})

					// Create filter for this specific kind
					kindFilter := nostr.Filter{
						Kinds: []int{kind},
						Until: &until,
						Limit: limit,
					}

					pageEvents := 0
					hasNewEvents := false

					// Create temporary pool for this operation
					tempPool := createTemporaryPool(ctx)

					for ev := range tempPool.SubManyEose(timeout, seedRelays, []nostr.Filter{kindFilter}) {
						// Use worker pool instead of creating unlimited goroutines
						select {
						case <-timeout.Done():
							logger.Warn("ARCHIVE", "Timeout reached, stopping pagination", map[string]interface{}{"kind": kind})
							goto nextKind
						case <-ctx.Done():
							logger.Warn("ARCHIVE", "Context cancelled, stopping pagination", map[string]interface{}{"kind": kind})
							goto nextKind
						default:
							// Deduplicate events globally
							if seenEvents[ev.Event.ID] {
								continue
							}
							seenEvents[ev.Event.ID] = true

							eventProcessor.QueueEvent(*ev.Event)
							pageEvents++
							kindEvents++
							totalEvents++
							hasNewEvents = true

							// Update until timestamp for next page
							if ev.Event.CreatedAt < until {
								until = ev.Event.CreatedAt
							}
						}
					}

				logger.Info("ARCHIVE", "Page processed", map[string]interface{}{
					"kind": kind, "page": pageCount, "page_events": pageEvents, "kind_total": kindEvents, "overall_total": totalEvents,
				})

					// Stop only when page is completely empty (0 events)
				if pageEvents == 0 {
					logger.Info("ARCHIVE", "Kind completed - empty page", map[string]interface{}{"kind": kind})
					break
					}

					// Stop if we've gone back too far (configurable limit)
				if until < maxArchiveTime {
					logger.Info("ARCHIVE", "Kind reached day limit", map[string]interface{}{"kind": kind, "max_days": archiveMaxDays, "until": until, "max_archive_time": maxArchiveTime})
					break
					}

					// Stop if no new events were found
				if !hasNewEvents {
					logger.Info("ARCHIVE", "Kind completed - no new events", map[string]interface{}{"kind": kind})
					break
					}

					// Small delay between pages to be nice to relays
					time.Sleep(200 * time.Millisecond)
				}

				logger.Info("ARCHIVE", "Kind completed", map[string]interface{}{"kind": kind, "events_processed": kindEvents})

				// Small delay between kinds to be nice to relays
				time.Sleep(500 * time.Millisecond)

			nextKind:
			}

			logger.Info("ARCHIVE", "Pagination completed", map[string]interface{}{
				"total_events": totalEvents,
			})

			logger.Info("ARCHIVE", "Archiving process completed", map[string]interface{}{
				"trusted_notes":   atomic.LoadInt64(&trustedNotes),
				"untrusted_notes": atomic.LoadInt64(&untrustedNotes),
			})
			metrics.UpdateLastArchiving()
		} else {
			logger.Info("WOT", "Web of trust will refresh", map[string]interface{}{"refresh_interval_hours": config.RefreshInterval})
			<-timeout.Done()
		}
	}()

	select {
	case <-timeout.Done():
		logger.Info("MAIN", "Restarting process")
	case <-done:
		logger.Info("ARCHIVE", "Archiving process completed")
	}
}

func archiveEvent(ctx context.Context, relay *khatru.Relay, ev nostr.Event) {
	trustNetworkMutex.RLock()
	trusted := trustNetworkMap[ev.PubKey]
	trustNetworkMutex.RUnlock()

	if trusted {
		wdb.Publish(ctx, ev)
		relay.BroadcastEvent(&ev)
		atomic.AddInt64(&trustedNotes, 1)
		atomic.AddInt64(&metrics.TrustedEvents, 1)
	} else {
		atomic.AddInt64(&untrustedNotes, 1)
		atomic.AddInt64(&metrics.UntrustedEvents, 1)
	}
	atomic.AddInt64(&metrics.TotalEvents, 1)
}

func deleteOldNotes(relay *khatru.Relay) error {
	ctx := context.TODO()

	if config.MaxAgeDays <= 0 {
		logger.Info("ARCHIVE", "MAX_AGE_DAYS disabled")
		return nil
	}

	maxAgeSecs := nostr.Timestamp(config.MaxAgeDays * 86400)
	oldAge := nostr.Now() - maxAgeSecs
	if oldAge <= 0 {
		logger.Warn("ARCHIVE", "MAX_AGE_DAYS too large")
		return nil
	}

	filter := nostr.Filter{
		Until: &oldAge,
		Kinds: []int{
			nostr.KindArticle,
			nostr.KindDeletion,
			nostr.KindFollowList,
			nostr.KindEncryptedDirectMessage,
			nostr.KindMuteList,
			nostr.KindReaction,
			nostr.KindRelayListMetadata,
			nostr.KindRepost,
			nostr.KindZapRequest,
			nostr.KindZap,
			nostr.KindTextNote,
		},
		Limit: 1000, // Process in batches to avoid memory issues
	}

	ch, err := relay.QueryEvents[0](ctx, filter)
	if err != nil {
		logger.Error("ARCHIVE", "Query error", map[string]interface{}{"error": err})
		return err
	}

	// Process events in batches to avoid memory issues
	batchSize := 100
	events := make([]*nostr.Event, 0, batchSize)
	count := 0

	for evt := range ch {
		events = append(events, evt)
		count++

		if len(events) >= batchSize {
			// Delete this batch
			for num_evt, del_evt := range events {
				for _, del := range relay.DeleteEvent {
				if err := del(ctx, del_evt); err != nil {
					logger.Error("ARCHIVE", "Error deleting note in batch", map[string]interface{}{"note_number": num_evt, "event_id": del_evt.ID, "error": err})
					return err
				}
				}
			}
			events = events[:0] // Reset slice but keep capacity
		}
	}

	// Delete remaining events
	if len(events) > 0 {
		for num_evt, del_evt := range events {
			for _, del := range relay.DeleteEvent {
			if err := del(ctx, del_evt); err != nil {
				logger.Error("ARCHIVE", "Error deleting note in final batch", map[string]interface{}{"note_number": num_evt, "event_id": del_evt.ID, "error": err})
				return err
			}
			}
		}
	}

	if count == 0 {
		logger.Info("ARCHIVE", "No old notes found")
	} else {
		logger.Info("ARCHIVE", "Old notes deleted", map[string]interface{}{"count": count, "old_age": oldAge})
	}

	return nil
}

func splitAndTrim(input string) []string {
	items := strings.Split(input, ",")
	for i, item := range items {
		items[i] = strings.TrimSpace(item)
	}
	return items
}

func isValidPubkey(pubkey string) bool {
	if len(pubkey) != 64 {
		return false
	}
	for _, c := range pubkey {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func isIgnored(pubkey string, ignoredPubkeys []string) bool {
	for _, ignored := range ignoredPubkeys {
		if pubkey == ignored {
			return true
		}
	}
	return false
}
