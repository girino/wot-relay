package main

import (
	"context"
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
	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
)

var (
	version string
)

type Config struct {
	RelayName         string
	RelayPubkey       string
	RelayDescription  string
	DBPath            string
	RelayURL          string
	IndexPath         string
	StaticPath        string
	RefreshInterval   int
	MinimumFollowers  int
	ArchivalSync      bool
	RelayContact      string
	RelayIcon         string
	MaxAgeDays        int
	ArchiveReactions  bool
	ArchiveMaxDays    int
	IgnoredPubkeys    []string
	WoTDepth          int
	ProfileMaxAgeDays int
}

var pool *nostr.SimplePool
var wdb nostr.RelayStore
var config Config

// var trustNetwork []string
var seedRelays []string
var trustNetworkMap map[string]bool
var trustNetworkMutex sync.RWMutex
var trustedNotes int64
var untrustedNotes int64
var archiveEventSemaphore = make(chan struct{}, 500) // Limit concurrent goroutines (increased from 100)
var indexTemplate *template.Template
var eventProcessor *EventProcessor
var batchProcessor *BatchProcessor
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

// Batch processor for database operations
type BatchProcessor struct {
	batchSize    int
	batchTimeout time.Duration
	events       []nostr.Event
	mutex        sync.Mutex
	ticker       *time.Ticker
	quit         chan struct{}
	wg           sync.WaitGroup
	relay        *khatru.Relay
}

func NewBatchProcessor(batchSize int, batchTimeout time.Duration) *BatchProcessor {
	return &BatchProcessor{
		batchSize:    batchSize,
		batchTimeout: batchTimeout,
		events:       make([]nostr.Event, 0, batchSize),
		quit:         make(chan struct{}),
	}
}

func (bp *BatchProcessor) Start(ctx context.Context, relay *khatru.Relay) {
	bp.relay = relay
	bp.ticker = time.NewTicker(bp.batchTimeout)
	bp.wg.Add(1)
	go bp.processBatches(ctx, relay)
}

func (bp *BatchProcessor) AddEvent(event nostr.Event) {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	bp.events = append(bp.events, event)

	// Process batch if it reaches the size limit
	if len(bp.events) >= bp.batchSize {
		go bp.processBatch(bp.events)
		bp.events = make([]nostr.Event, 0, bp.batchSize)
	}
}

func (bp *BatchProcessor) processBatches(ctx context.Context, relay *khatru.Relay) {
	defer bp.wg.Done()

	for {
		select {
		case <-bp.ticker.C:
			bp.mutex.Lock()
			if len(bp.events) > 0 {
				events := make([]nostr.Event, len(bp.events))
				copy(events, bp.events)
				bp.events = bp.events[:0]
				bp.mutex.Unlock()
				go bp.processBatch(events)
			} else {
				bp.mutex.Unlock()
			}
		case <-bp.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (bp *BatchProcessor) processBatch(events []nostr.Event) {
	for _, event := range events {
		archiveEvent(context.Background(), bp.relay, event)
	}
}

func (bp *BatchProcessor) Stop() {
	bp.ticker.Stop()
	close(bp.quit)
	bp.wg.Wait()
}

// Worker pool for event processing
type EventProcessor struct {
	workerCount int
	eventChan   chan nostr.Event
	quit        chan struct{}
	wg          sync.WaitGroup
}

func NewEventProcessor(workerCount int) *EventProcessor {
	return &EventProcessor{
		workerCount: workerCount,
		eventChan:   make(chan nostr.Event, workerCount*50), // Buffer for 50x worker count
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
			archiveEvent(ctx, relay, event)
		case <-ep.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (ep *EventProcessor) ProcessEvent(event nostr.Event) {
	// Block until we can send the event - don't drop any events
	// Check if channel is closed to avoid panic
	select {
	case ep.eventChan <- event:
		// Event sent successfully
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
	log.Println("🚀 booting up web of trust relay")
	relay := khatru.NewRelay()
	ctx, cancel := context.WithCancel(context.Background())
	pool = nostr.NewSimplePool(ctx)
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

	db := getDB()
	if err := db.Init(); err != nil {
		panic(err)
	}

	// Configure connection pooling and add custom indexes
	if err := optimizeDatabase(&db); err != nil {
		log.Printf("Warning: Database optimization failed: %v", err)
	}

	wdb = eventstore.RelayWrapper{Store: &db}

	relay.RejectEvent = append(relay.RejectEvent,
		policies.RejectEventsWithBase64Media,
		policies.EventIPRateLimiter(50, time.Minute*1, 300),
	)

	relay.RejectFilter = append(relay.RejectFilter,
		policies.NoEmptyFilters,
		policies.NoComplexFilters,
	)

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

	seedRelays = []string{
		"wss://relay.primal.net",
		"wss://relay.damus.io",
		"wss://nos.lol",
		"wss://wot.utxo.one/",
		// "wss://relay.snort.social",
		// "wss://wot.girino.org",
		// "wss://nostr.girino.org",
		// "wss://relay.nostr.band",
		// "wss://eden.nostr.land", // Known to have issues with large batches
		// "wss://nostr.oxtr.dev/",
		"wss://nostr.mom",
		// "wss://purplepag.es",
		// "wss://purplerelay.com",
		// "wss://relayable.org",
		// "wss://relay.nostr.bg",
		// "wss://no.str.cr",
		// "wss://nostr21.com",
		// "wss://nostrue.com",
		// "wss://relay.siamstr.com",
		// "wss://haven.girino.org/outbox",
		// "wss://haven.girino.org/inbox",
	}

	// Initialize performance components
	eventProcessor = NewEventProcessor(2000) // 2000 workers for event processing
	eventProcessor.Start(ctx, relay)

	batchProcessor = NewBatchProcessor(50, 1*time.Second) // Batch 50 events or wait 1 second
	batchProcessor.Start(ctx, relay)

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
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	// Create server with graceful shutdown
	server := &http.Server{
		Addr:    ":3334",
		Handler: relay,
	}

	go func() {
		log.Println("🎉 relay running on port :3334")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("🛑 shutting down gracefully...")

	// Cancel the main context to stop all background processes
	cancel()

	// Give background processes a moment to finish gracefully
	log.Println("🛑 waiting for background processes to finish...")
	time.Sleep(2 * time.Second)

	// Stop processors first
	if eventProcessor != nil {
		log.Println("🛑 stopping event processor...")
		eventProcessor.Stop()
	}

	if batchProcessor != nil {
		log.Println("🛑 stopping batch processor...")
		batchProcessor.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("✅ server stopped")
}

func monitorResources() {
	for {
		func() {
			var m runtime.MemStats
			log.Printf("Number of Goroutines: %d", runtime.NumGoroutine())
			runtime.ReadMemStats(&m)
			log.Printf("Alloc = %v MiB, Sys = %v MiB, NumGC = %v",
				m.Alloc/1024/1024,
				m.Sys/1024/1024,
				m.NumGC)

			trustNetworkMutex.RLock()
			networkSize := len(trustNetworkMap)
			trustNetworkMutex.RUnlock()

			log.Printf("🫂 network size: %d, trusted notes: %d, untrusted notes: %d",
				networkSize,
				atomic.LoadInt64(&trustedNotes),
				atomic.LoadInt64(&untrustedNotes))

			// Database performance monitoring
			if wdb != nil {
				if db, ok := wdb.(*eventstore.RelayWrapper); ok {
					if sqliteDB, ok := db.Store.(*sqlite3.SQLite3Backend); ok {
						stats := sqliteDB.Stats()
						log.Printf("📊 DB connections: open=%d, inUse=%d, idle=%d, waitCount=%d",
							stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
					}
				}
			}

			// Memory monitoring
			if memoryMonitor != nil {
				exceeded, message := memoryMonitor.CheckMemory()
				if exceeded {
					log.Printf("🚨 %s", message)
					// Force garbage collection when memory limit exceeded
					runtime.GC()
				} else if message != "" {
					log.Printf("⚠️ %s", message)
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
	log.Println("🔄 refresh interval set to", refreshInterval, "hours")

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

	if os.Getenv("PROFILE_MAX_AGE_DAYS") == "" {
		os.Setenv("PROFILE_MAX_AGE_DAYS", "30")
	}

	minimumFollowers, _ := strconv.Atoi(os.Getenv("MINIMUM_FOLLOWERS"))
	maxAgeDays, _ := strconv.Atoi(os.Getenv("MAX_AGE_DAYS"))
	archiveMaxDays, _ := strconv.Atoi(os.Getenv("ARCHIVE_MAX_DAYS"))
	profileMaxAgeDays, _ := strconv.Atoi(os.Getenv("PROFILE_MAX_AGE_DAYS"))
	woTDepth, _ := strconv.Atoi(os.Getenv("WOT_DEPTH"))

	config := Config{
		RelayName:         getEnv("RELAY_NAME"),
		RelayPubkey:       getEnv("RELAY_PUBKEY"),
		RelayDescription:  getEnv("RELAY_DESCRIPTION"),
		RelayContact:      getEnv("RELAY_CONTACT"),
		RelayIcon:         getEnv("RELAY_ICON"),
		DBPath:            getEnv("DB_PATH"),
		RelayURL:          getEnv("RELAY_URL"),
		IndexPath:         getEnv("INDEX_PATH"),
		StaticPath:        getEnv("STATIC_PATH"),
		RefreshInterval:   refreshInterval,
		MinimumFollowers:  minimumFollowers,
		ArchivalSync:      getEnv("ARCHIVAL_SYNC") == "TRUE",
		MaxAgeDays:        maxAgeDays,
		ArchiveReactions:  getEnv("ARCHIVE_REACTIONS") == "TRUE",
		ArchiveMaxDays:    archiveMaxDays,
		IgnoredPubkeys:    ignoredPubkeys,
		WoTDepth:          woTDepth,
		ProfileMaxAgeDays: profileMaxAgeDays,
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

	log.Println("🌐 building new trust network map")

	for pubkey, count := range pubkeyFollowerCount {
		if count >= config.MinimumFollowers {
			myTrustNetworkMap[pubkey] = true
		}
	}

	// Thread-safe update of trust network map
	trustNetworkMutex.Lock()
	trustNetworkMap = myTrustNetworkMap
	trustNetworkMutex.Unlock()

	log.Println("🌐 trust network map updated with", len(myTrustNetworkMap), "keys")
}

func refreshProfiles(ctx context.Context) {
	log.Println("👤 refreshing profiles")

	trustNetworkMutex.RLock()
	trustNetwork := make([]string, 0, len(trustNetworkMap))
	for pubkey := range trustNetworkMap {
		trustNetwork = append(trustNetwork, pubkey)
	}
	trustNetworkMutex.RUnlock()

	// Find pubkeys that need profile refresh (missing profiles only)
	pubkeysToRefresh := make([]string, 0)

	log.Printf("👤 checking %d profiles for refresh (missing profiles only)", len(trustNetwork))

	for _, pubkey := range trustNetwork {
		// Check if profile exists at all
		needsRefresh := true

		// Query for existing profile
		filter := nostr.Filter{
			Authors: []string{pubkey},
			Kinds:   []int{nostr.KindProfileMetadata},
			Limit:   1,
		}

		// Quick check with short timeout
		checkCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		ch, err := wdb.QueryEvents(checkCtx, filter)
		if err == nil {
			for range ch {
				// If we found any profile, we don't need to refresh
				needsRefresh = false
				break
			}
		}
		cancel()

		if needsRefresh {
			pubkeysToRefresh = append(pubkeysToRefresh, pubkey)
		}
	}

	log.Printf("👤 found %d profiles that need refresh (out of %d total)", len(pubkeysToRefresh), len(trustNetwork))

	if len(pubkeysToRefresh) == 0 {
		log.Println("👤 all profiles are up to date")
		return
	}

	// Refresh only the profiles that need updating
	stepSize := 200
	for i := 0; i < len(pubkeysToRefresh); i += stepSize {
		log.Printf("👤 refreshing profiles from %d to %d of %d", i, i+stepSize, len(pubkeysToRefresh))

		// force cancel context every time
		func() {
			timeout, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()

			end := i + stepSize
			if end > len(pubkeysToRefresh) {
				end = len(pubkeysToRefresh)
			}

			filters := []nostr.Filter{{
				Authors: pubkeysToRefresh[i:end],
				Kinds:   []int{nostr.KindProfileMetadata},
			}}

			for ev := range pool.SubManyEose(timeout, seedRelays, filters) {
				wdb.Publish(ctx, *ev.Event)
			}
		}()
	}
	log.Printf("👤 profiles refreshed: %d out of %d total", len(pubkeysToRefresh), len(trustNetwork))
}

func refreshTrustNetwork(ctx context.Context, relay *khatru.Relay) {

	runTrustNetworkRefresh := func(wotDepth int) map[string]int {
		newPubkeyFollowerCount := make(map[string]int)
		lastPubkeyFollowerCount := make(map[string]int)
		// initializes with seed pubkey
		newPubkeyFollowerCount[config.RelayPubkey]++

		log.Println("🌐 building web of trust graph")
		for j := 0; j < wotDepth; j++ {
			log.Println("🌐 WoT depth", j)
			oneHopNetwork := make([]string, 0)
			for pubkey := range newPubkeyFollowerCount {
				if _, exists := lastPubkeyFollowerCount[pubkey]; !exists {
					if isValidPubkey(pubkey) {
						oneHopNetwork = append(oneHopNetwork, pubkey)
					} else {
						log.Println("invalid pubkey in one hop network: ", pubkey)
					}
				}
			}
			lastPubkeyFollowerCount = make(map[string]int)
			for pubkey, count := range newPubkeyFollowerCount {
				lastPubkeyFollowerCount[pubkey] = count
			}

			stepSize := 300
			for i := 0; i < len(oneHopNetwork); i += stepSize {
				log.Println("🌐 fetching followers from", i, "to", i+stepSize, "of", len(oneHopNetwork))

				end := i + stepSize
				if end > len(oneHopNetwork) {
					end = len(oneHopNetwork)
				}

				filters := []nostr.Filter{{
					Authors: oneHopNetwork[i:end],
					Kinds:   []int{nostr.KindFollowList, nostr.KindRelayListMetadata, nostr.KindProfileMetadata},
				}}

				func() { // avoid "too many concurrent reqs" error
					timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					for ev := range pool.SubManyEose(timeout, seedRelays, filters) {
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
		log.Println("🫂  total network size:", len(newPubkeyFollowerCount))
		return newPubkeyFollowerCount
	}

	// build partial trust network if woTDepth is > 2 and its first run
	if config.WoTDepth > 2 && len(trustNetworkMap) <= 1 {

		updateTrustNetworkFilter(runTrustNetworkRefresh(2))
	}

	archivingCompleted := false

	for {
		updateTrustNetworkFilter(runTrustNetworkRefresh(config.WoTDepth))
		deleteOldNotes(relay)
		refreshProfiles(ctx)

		// Run archiving only once, after the first full WoT network is built and profiles are refreshed
		if !archivingCompleted {
			archiveTrustedNotes(ctx, relay)
			archivingCompleted = true
		}

		// Wait for the configured refresh interval before next cycle
		log.Printf("🔄 web of trust will refresh in %d hours", config.RefreshInterval)
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
			log.Println("📦 starting archiving process...")

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

			log.Println("📦 archiving trusted notes...")

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
				log.Printf("📦 processing kind %d events", kind)

				// Use nak-style pagination for this specific kind
				until := nostr.Now()
				limit := 500 // Reasonable limit per request
				kindEvents := 0
				pageCount := 0

				for {
					pageCount++
					log.Printf("📦 kind %d, page %d (until: %d, limit: %d)", kind, pageCount, until, limit)

					// Create filter for this specific kind
					kindFilter := nostr.Filter{
						Kinds: []int{kind},
						Until: &until,
						Limit: limit,
					}

					pageEvents := 0
					hasNewEvents := false

					for ev := range pool.SubManyEose(timeout, seedRelays, []nostr.Filter{kindFilter}) {
						// Use worker pool instead of creating unlimited goroutines
						select {
						case <-timeout.Done():
							log.Printf("📦 timeout reached, stopping pagination for kind %d", kind)
							goto nextKind
						case <-ctx.Done():
							log.Printf("📦 context cancelled, stopping pagination for kind %d", kind)
							goto nextKind
						default:
							// Deduplicate events globally
							if seenEvents[ev.Event.ID] {
								continue
							}
							seenEvents[ev.Event.ID] = true

							eventProcessor.ProcessEvent(*ev.Event)
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

					log.Printf("📦 kind %d, page %d: processed %d events (kind total: %d, overall total: %d)",
						kind, pageCount, pageEvents, kindEvents, totalEvents)

					// Stop only when page is completely empty (0 events)
					if pageEvents == 0 {
						log.Printf("📦 kind %d completed: got 0 events (page empty)", kind)
						break
					}

					// Stop if we've gone back too far (configurable limit)
					if until < maxArchiveTime {
						log.Printf("📦 kind %d reached %d-day limit (until: %d < %d)", kind, archiveMaxDays, until, maxArchiveTime)
						break
					}

					// Stop if no new events were found
					if !hasNewEvents {
						log.Printf("📦 kind %d completed: no new events found", kind)
						break
					}

					// Small delay between pages to be nice to relays
					time.Sleep(200 * time.Millisecond)
				}

				log.Printf("📦 kind %d completed: processed %d events", kind, kindEvents)

				// Small delay between kinds to be nice to relays
				time.Sleep(500 * time.Millisecond)

			nextKind:
			}

			log.Printf("📦 pagination completed: %d total events across all kinds", totalEvents)

			log.Printf("📦 archived %d trusted notes and discarded %d untrusted notes",
				atomic.LoadInt64(&trustedNotes),
				atomic.LoadInt64(&untrustedNotes))

			log.Println("📦 archiving completed")
		} else {
			log.Println("🔄 web of trust will refresh in", config.RefreshInterval, "hours")
			<-timeout.Done()
		}
	}()

	select {
	case <-timeout.Done():
		log.Println("restarting process")
	case <-done:
		log.Println("📦 archiving process completed")
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
	} else {
		atomic.AddInt64(&untrustedNotes, 1)
	}
}

func deleteOldNotes(relay *khatru.Relay) error {
	ctx := context.TODO()

	if config.MaxAgeDays <= 0 {
		log.Printf("MAX_AGE_DAYS disabled")
		return nil
	}

	maxAgeSecs := nostr.Timestamp(config.MaxAgeDays * 86400)
	oldAge := nostr.Now() - maxAgeSecs
	if oldAge <= 0 {
		log.Printf("MAX_AGE_DAYS too large")
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
		log.Printf("query error %s", err)
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
						log.Printf("error deleting note %d of batch. event id: %s", num_evt, del_evt.ID)
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
					log.Printf("error deleting note %d of final batch. event id: %s", num_evt, del_evt.ID)
					return err
				}
			}
		}
	}

	if count == 0 {
		log.Println("0 old notes found")
	} else {
		log.Printf("%d old (until %d) notes deleted", count, oldAge)
	}

	return nil
}

//	func getDB() badger.BadgerBackend {
//		return badger.BadgerBackend{
//			Path: getEnv("DB_PATH"),
//		}
//	}
func optimizeDatabase(db *sqlite3.SQLite3Backend) error {
	// Configure connection pooling for better performance
	db.SetMaxOpenConns(25)                 // Maximum number of open connections
	db.SetMaxIdleConns(5)                  // Maximum number of idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Maximum connection lifetime

	// Add custom indexes for specific query patterns used by the relay
	customIndexes := []string{
		// Composite index for pubkey + time queries (most common)
		`CREATE INDEX IF NOT EXISTS pubkey_time_idx ON event(pubkey, created_at DESC)`,

		// Composite index for kind + pubkey + time queries
		`CREATE INDEX IF NOT EXISTS kind_pubkey_time_idx ON event(kind, pubkey, created_at DESC)`,

		// Index for time-based queries (used in deleteOldNotes)
		`CREATE INDEX IF NOT EXISTS created_at_idx ON event(created_at)`,

		// Index for pubkey + kind queries (common in filters)
		`CREATE INDEX IF NOT EXISTS pubkey_kind_idx ON event(pubkey, kind)`,

		// Index for tags JSON queries (if needed for complex tag filtering)
		`CREATE INDEX IF NOT EXISTS tags_content_idx ON event(tags, content)`,
	}

	log.Println("🔧 Adding custom database indexes...")
	for i, idx := range customIndexes {
		if _, err := db.Exec(idx); err != nil {
			log.Printf("Warning: Failed to create index %d: %v", i+1, err)
			// Continue with other indexes even if one fails
		}
	}

	log.Println("✅ Database optimization completed")
	return nil
}

func getDB() sqlite3.SQLite3Backend {
	dbPath := getEnv("DB_PATH")

	// Add SQLite performance optimizations to URL
	// WAL mode for better concurrency, larger cache, memory temp store, etc.
	optimizedURL := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=MEMORY&_mmap_size=268435456&_busy_timeout=30000", dbPath)

	return sqlite3.SQLite3Backend{
		DatabaseURL:       optimizedURL,
		QueryLimit:        1000, // Increase from default 100
		QueryIDsLimit:     2000, // Increase from default 500
		QueryAuthorsLimit: 2000, // Increase from default 500
		QueryKindsLimit:   50,   // Increase from default 10
		QueryTagsLimit:    50,   // Increase from default 10
	}
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
