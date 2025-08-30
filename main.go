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
	IgnoredPubkeys   []string
	WoTDepth         int
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
var archiveEventSemaphore = make(chan struct{}, 100) // Limit concurrent goroutines
var indexTemplate *template.Template

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
	ctx := context.Background()
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
		"wss://relay.damus.io",
		"wss://nos.lol",
		"wss://relay.nostr.band",
		"wss://eden.nostr.land",
		"wss://nostr.oxtr.dev/",
		"wss://wot.utxo.one/",
		"wss://nostr.mom",
		"wss://purplepag.es",
		"wss://purplerelay.com",
		"wss://relay.snort.social",
		"wss://relayable.org",
		"wss://relay.primal.net",
		"wss://relay.nostr.bg",
		"wss://no.str.cr",
		"wss://nostr21.com",
		"wss://nostrue.com",
		"wss://relay.siamstr.com",
	}

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

	minimumFollowers, _ := strconv.Atoi(os.Getenv("MINIMUM_FOLLOWERS"))
	maxAgeDays, _ := strconv.Atoi(os.Getenv("MAX_AGE_DAYS"))
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

	stepSize := 200
	for i := 0; i < len(trustNetwork); i += stepSize {
		log.Println("👤 refreshing profiles from", i, "to", i+stepSize, "of", len(trustNetwork))

		// force cancel context every time
		func() {
			timeout, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()

			end := i + 200
			if end > len(trustNetwork) {
				end = len(trustNetwork)
			}

			filters := []nostr.Filter{{
				Authors: trustNetwork[i:end],
				Kinds:   []int{nostr.KindProfileMetadata},
			}}

			for ev := range pool.SubManyEose(timeout, seedRelays, filters) {
				wdb.Publish(ctx, *ev.Event)
			}
		}()
	}
	log.Println("👤 profiles refreshed: ", len(trustNetwork))
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
						for _, contact := range ev.Event.Tags.GetAll([]string{"p"}) {
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

	for {
		updateTrustNetworkFilter(runTrustNetworkRefresh(config.WoTDepth))
		deleteOldNotes(relay)
		archiveTrustedNotes(ctx, relay)
	}
}

func archiveTrustedNotes(ctx context.Context, relay *khatru.Relay) {
	timeout, cancel := context.WithTimeout(ctx, time.Duration(config.RefreshInterval)*time.Hour)
	defer cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)
		if config.ArchivalSync {
			go refreshProfiles(ctx)

			var filters []nostr.Filter
			if config.ArchiveReactions {
				filters = []nostr.Filter{{
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
				}}
			} else {
				filters = []nostr.Filter{{
					Kinds: []int{
						nostr.KindArticle,
						nostr.KindDeletion,
						nostr.KindFollowList,
						nostr.KindEncryptedDirectMessage,
						nostr.KindMuteList,
						nostr.KindRelayListMetadata,
						nostr.KindRepost,
						nostr.KindZapRequest,
						nostr.KindZap,
						nostr.KindTextNote,
					},
				}}
			}

			log.Println("📦 archiving trusted notes...")

			for ev := range pool.SubMany(timeout, seedRelays, filters) {
				// Use semaphore to limit concurrent goroutines
				select {
				case archiveEventSemaphore <- struct{}{}:
					go func(event nostr.Event) {
						defer func() { <-archiveEventSemaphore }()
						select {
						case <-timeout.Done():
							return
						default:
							archiveEvent(ctx, relay, event)
						}
					}(*ev.Event)
				case <-timeout.Done():
					return
				}
			}

			log.Printf("📦 archived %d trusted notes and discarded %d untrusted notes",
				atomic.LoadInt64(&trustedNotes),
				atomic.LoadInt64(&untrustedNotes))
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
func getDB() sqlite3.SQLite3Backend {
	return sqlite3.SQLite3Backend{
		DatabaseURL: getEnv("DB_PATH"),
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
