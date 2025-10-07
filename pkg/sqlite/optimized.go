package sqlite

import (
	"fmt"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/sqlite3"
	"github.com/girino/wot-relay/pkg/logger"
	"github.com/jmoiron/sqlx"
)

// OptimizedSQLiteConfig holds configuration for optimized SQLite setup
type OptimizedSQLiteConfig struct {
	DatabaseURL       string
	QueryLimit        int
	QueryIDsLimit     int
	QueryAuthorsLimit int
	QueryKindsLimit   int
	QueryTagsLimit    int
}

// OptimizedSQLiteBackend wraps sqlite3.SQLite3Backend with optimizations
type OptimizedSQLiteBackend struct {
	*sqlite3.SQLite3Backend
}

// NewOptimizedSQLiteBackend creates a new optimized SQLite backend
func NewOptimizedSQLiteBackend(dbPath string) *OptimizedSQLiteBackend {
	// Add SQLite performance optimizations to URL
	// WAL mode for better concurrency, larger cache, memory temp store, etc.
	optimizedURL := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=20000&_temp_store=MEMORY&_mmap_size=536870912&_busy_timeout=60000&_timeout=30000", dbPath)

	backend := sqlite3.SQLite3Backend{ // VALUE not pointer!
		DatabaseURL:       optimizedURL,
		QueryLimit:        1000, // Increase from default 100
		QueryIDsLimit:     2000, // Increase from default 500
		QueryAuthorsLimit: 2000, // Increase from default 500
		QueryKindsLimit:   50,   // Increase from default 10
		QueryTagsLimit:    50,   // Increase from default 10
	}

	return &OptimizedSQLiteBackend{
		SQLite3Backend: &backend, // Now take address of the value
	}
}

// Init initializes the database and applies optimizations
func (o *OptimizedSQLiteBackend) Init() error {
	logger.Info("SQLITE", "OptimizedSQLiteBackend initialization starting", map[string]interface{}{"database_url": o.SQLite3Backend.DatabaseURL})

	// Initialize the underlying SQLite backend
	if err := o.SQLite3Backend.Init(); err != nil {
		logger.Error("SQLITE", "SQLite3Backend initialization failed", map[string]interface{}{"error": err})
		return err
	}

	logger.Info("SQLITE", "SQLite3Backend initialization succeeded", map[string]interface{}{"db_connected": o.SQLite3Backend.DB != nil})

	// Note: OptimizeDatabase call removed as it takes too long on startup
	// The database indexes and pragmas are already set via connection URL parameters
	return nil
}

// QueryEvents explicitly delegates to the backend with debug logging
// QueryEvents is no longer needed; use the underlying SQLite3Backend's implementation directly.

// Ensure OptimizedSQLiteBackend implements eventstore.Store interface
var _ eventstore.Store = (*OptimizedSQLiteBackend)(nil)

// OptimizeDatabase applies SQLite performance optimizations and custom indexes
func OptimizeDatabase(db *sqlx.DB) error {
	// Configure connection pooling for better performance
	db.SetMaxOpenConns(25)                 // Maximum number of open connections
	db.SetMaxIdleConns(5)                  // Maximum number of idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Maximum connection lifetime

	// Add custom indexes for specific query patterns used by the relay
	customIndexes := []string{
		// Primary composite index for most common query pattern: authors + kinds + time
		// This covers queries like: WHERE pubkey IN (...) AND kind IN (...) ORDER BY created_at DESC
		`CREATE INDEX IF NOT EXISTS authors_kinds_time_idx ON event(pubkey, kind, created_at DESC)`,

		// Optimized index for WoT queries: kind 1984 + authors + time
		// Covers: WHERE kind = 1984 AND pubkey IN (...) ORDER BY created_at DESC
		`CREATE INDEX IF NOT EXISTS kind1984_authors_time_idx ON event(kind, pubkey, created_at DESC) WHERE kind = 1984`,

		// Optimized index for profile queries: kind 0 + authors + time
		// Covers: WHERE kind = 0 AND pubkey IN (...) ORDER BY created_at DESC
		`CREATE INDEX IF NOT EXISTS kind0_authors_time_idx ON event(kind, pubkey, created_at DESC) WHERE kind = 0`,

		// Optimized index for follow list queries: kind 3 + authors + time
		// Covers: WHERE kind = 3 AND pubkey IN (...) ORDER BY created_at DESC
		`CREATE INDEX IF NOT EXISTS kind3_authors_time_idx ON event(kind, pubkey, created_at DESC) WHERE kind = 3`,

		// Index for time-based queries (used in deleteOldNotes and archiving)
		`CREATE INDEX IF NOT EXISTS created_at_idx ON event(created_at)`,

		// Index for pubkey-only queries (when no kind filter)
		`CREATE INDEX IF NOT EXISTS pubkey_time_idx ON event(pubkey, created_at DESC)`,

		// Index for kind-only queries (when no author filter)
		`CREATE INDEX IF NOT EXISTS kind_time_idx ON event(kind, created_at DESC)`,

		// Index for tag queries with #p tags (WoT queries)
		`CREATE INDEX IF NOT EXISTS tags_p_idx ON event(tags) WHERE tags LIKE '%"#p"%'`,

		// Index for complex multi-kind queries (archiving)
		`CREATE INDEX IF NOT EXISTS multi_kind_time_idx ON event(kind, created_at DESC) WHERE kind IN (1,6,16,1984,9735,6969,1040,1010,1111)`,
	}

	logger.Info("SQLITE", "Adding custom database indexes")
	for i, idx := range customIndexes {
		if _, err := db.Exec(idx); err != nil {
			logger.Warn("SQLITE", "Failed to create index", map[string]interface{}{"index_number": i + 1, "error": err})
			// Continue with other indexes even if one fails
		}
	}

	// Additional SQLite optimizations for better query performance
	// Note: Removed PRAGMA optimize and ANALYZE as they can take very long on large databases
	optimizationQueries := []string{
		// Increase cache size for better performance with large datasets
		`PRAGMA cache_size = -128000`, // 128MB cache (increased from 64MB)

		// Enable WAL mode for better concurrency
		`PRAGMA journal_mode = WAL`,

		// Increase page size for better I/O performance
		`PRAGMA page_size = 4096`,

		// Enable foreign key constraints (if needed)
		`PRAGMA foreign_keys = ON`,

		// Set synchronous mode for better performance (with some risk)
		`PRAGMA synchronous = NORMAL`,

		// Enable memory-mapped I/O for better performance
		`PRAGMA mmap_size = 536870912`, // 512MB (increased from 256MB)

		// Increase busy timeout to handle concurrent access better
		`PRAGMA busy_timeout = 30000`, // 30 seconds

		// Set temp store to memory for better performance
		`PRAGMA temp_store = MEMORY`,
	}

	logger.Info("SQLITE", "Applying SQLite performance optimizations (fast startup)")
	for i, query := range optimizationQueries {
		if _, err := db.Exec(query); err != nil {
			logger.Warn("SQLITE", "Failed to apply optimization", map[string]interface{}{"optimization_number": i + 1, "error": err})
			// Continue with other optimizations even if one fails
		}
	}

	logger.Info("SQLITE", "Database optimization completed")
	return nil
}
