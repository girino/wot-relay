package newsqlite3

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/fiatjaf/eventstore"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	_ "github.com/mattn/go-sqlite3"
)

const (
	queryLimit              = 100
	queryIDsLimit           = 500
	queryAuthorsLimit       = 500
	queryKindsLimit         = 10
	queryTagsLimit          = 10
	currentMigrationVersion = 3
)

var _ eventstore.Store = (*SQLite3Backend)(nil)

func (b *SQLite3Backend) Init() error {
	db, err := sqlx.Connect("sqlite3", b.DatabaseURL)
	if err != nil {
		return err
	}

	db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)
	b.DB = db

	// Run migrations
	if err := b.runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Set default query limits
	if b.QueryLimit == 0 {
		b.QueryLimit = queryLimit
	}
	if b.QueryIDsLimit == 0 {
		b.QueryIDsLimit = queryIDsLimit
	}
	if b.QueryAuthorsLimit == 0 {
		b.QueryAuthorsLimit = queryAuthorsLimit
	}
	if b.QueryKindsLimit == 0 {
		b.QueryKindsLimit = queryKindsLimit
	}
	if b.QueryTagsLimit == 0 {
		b.QueryTagsLimit = queryTagsLimit
	}

	// Start periodic maintenance if interval is set
	b.StartPeriodicMaintenance()

	return nil
}

// getCurrentVersion returns the current migration version from the database
func (b *SQLite3Backend) getCurrentVersion() (int, error) {
	// First, check if migrations table exists
	var tableName string
	err := b.DB.QueryRow(`
		SELECT name FROM sqlite_master 
		WHERE type='table' AND name='migrations'
	`).Scan(&tableName)

	if err == sql.ErrNoRows {
		// Migrations table doesn't exist yet, we're at version -1
		return -1, nil
	}
	if err != nil {
		return -1, fmt.Errorf("failed to check migrations table: %w", err)
	}

	// Get current version
	var version int
	err = b.DB.QueryRow("SELECT version FROM migrations ORDER BY version DESC LIMIT 1").Scan(&version)
	if err == sql.ErrNoRows {
		// Table exists but no migrations recorded yet
		return -1, nil
	}
	if err != nil {
		return -1, fmt.Errorf("failed to get current version: %w", err)
	}

	return version, nil
}

// setVersion records a migration version in the migrations table
func (b *SQLite3Backend) setVersion(version int) error {
	_, err := b.DB.Exec("INSERT INTO migrations (version, applied_at) VALUES (?, datetime('now'))", version)
	if err != nil {
		return fmt.Errorf("failed to set migration version %d: %w", version, err)
	}
	return nil
}

// checkEventTableExists checks if the event table exists
func (b *SQLite3Backend) checkEventTableExists() (bool, error) {
	var tableName string
	err := b.DB.QueryRow(`
		SELECT name FROM sqlite_master 
		WHERE type='table' AND name='event'
	`).Scan(&tableName)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check event table: %w", err)
	}
	return true, nil
}

// runMigrations executes all pending database migrations
func (b *SQLite3Backend) runMigrations() error {
	// Create migrations table if it doesn't exist
	_, err := b.DB.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	currentVersion, err := b.getCurrentVersion()
	if err != nil {
		return err
	}

	// Check if this is an existing fiatjaf/eventstore database
	// (event table exists but no migrations recorded)
	if currentVersion < 0 {
		eventTableExists, err := b.checkEventTableExists()
		if err != nil {
			return err
		}

		if eventTableExists {
			// This is an existing database from fiatjaf/eventstore
			// Mark migration 0 as done without running it
			if err := b.setVersion(0); err != nil {
				return err
			}
			currentVersion = 0
		}
	}

	// Run migrations in order
	if currentVersion < 0 {
		if err := b.migration0(); err != nil {
			return fmt.Errorf("migration 0 failed: %w", err)
		}
		if err := b.setVersion(0); err != nil {
			return err
		}
		currentVersion = 0
	}

	if currentVersion < 1 {
		if err := b.migration1(); err != nil {
			return fmt.Errorf("migration 1 failed: %w", err)
		}
		if err := b.setVersion(1); err != nil {
			return err
		}
		currentVersion = 1
	}

	if currentVersion < 2 {
		if err := b.migration2(); err != nil {
			return fmt.Errorf("migration 2 failed: %w", err)
		}
		if err := b.setVersion(2); err != nil {
			return err
		}
		currentVersion = 2
	}

	if currentVersion < 3 {
		if err := b.migration3(); err != nil {
			return fmt.Errorf("migration 3 failed: %w", err)
		}
		if err := b.setVersion(3); err != nil {
			return err
		}
		currentVersion = 3
	}

	return nil
}

// migration0 creates the event table and indexes
func (b *SQLite3Backend) migration0() error {
	fmt.Println("Running migration 0...")

	fmt.Println("  Creating event table...")
	if _, err := b.DB.Exec(`CREATE TABLE IF NOT EXISTS event (
		id text NOT NULL,
		pubkey text NOT NULL,
		created_at integer NOT NULL,
		kind integer NOT NULL,
		tags jsonb NOT NULL,
		content text NOT NULL,
		sig text NOT NULL)`); err != nil {
		return fmt.Errorf("failed to create event table: %w", err)
	}

	indexes := map[string]string{
		"ididx":        `CREATE UNIQUE INDEX IF NOT EXISTS ididx ON event(id)`,
		"pubkeyprefix": `CREATE INDEX IF NOT EXISTS pubkeyprefix ON event(pubkey)`,
		"timeidx":      `CREATE INDEX IF NOT EXISTS timeidx ON event(created_at DESC)`,
		"kindidx":      `CREATE INDEX IF NOT EXISTS kindidx ON event(kind)`,
		"kindtimeidx":  `CREATE INDEX IF NOT EXISTS kindtimeidx ON event(kind,created_at DESC)`,
	}

	for name, ddl := range indexes {
		fmt.Printf("  Creating index %s...\n", name)
		if _, err := b.DB.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create index %s: %w", name, err)
		}
	}

	fmt.Println("Migration 0 complete")
	return nil
}

// migration1 removes old indexes from previous sqlite implementation
func (b *SQLite3Backend) migration1() error {
	fmt.Println("Running migration 1...")

	// List of old indexes to remove from previous implementation
	oldIndexes := []string{
		"authors_kinds_time_idx",
		"kind1984_authors_time_idx",
		"kind0_authors_time_idx",
		"kind3_authors_time_idx",
		"created_at_idx",
		"pubkey_time_idx",
		"kind_time_idx",
		"tags_p_idx",
		"multi_kind_time_idx",
	}

	for _, indexName := range oldIndexes {
		fmt.Printf("  Dropping index %s if it exists...\n", indexName)
		_, err := b.DB.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", indexName))
		if err != nil {
			return fmt.Errorf("failed to drop index %s: %w", indexName, err)
		}
	}

	fmt.Println("Migration 1 complete")
	return nil
}

// migration2 enables foreign keys and creates the tag table
func (b *SQLite3Backend) migration2() error {
	fmt.Println("Running migration 2...")

	// Disable foreign key constraints during migration for better performance
	fmt.Println("  Temporarily disabling foreign key constraints for faster migration...")
	if _, err := b.DB.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("failed to disable foreign keys: %w", err)
	}

	// Drop the tag table if it exists to start fresh
	fmt.Println("  Dropping tag table if it exists...")
	if _, err := b.DB.Exec("DROP TABLE IF EXISTS tag"); err != nil {
		return fmt.Errorf("failed to drop tag table: %w", err)
	}

	fmt.Println("  Creating tag table...")
	if _, err := b.DB.Exec(`CREATE TABLE tag (
		event_id TEXT NOT NULL,
		tag_order INTEGER NOT NULL,
		identifier TEXT NOT NULL,
		first_data TEXT,
		tag_data JSONB NOT NULL,
		PRIMARY KEY (event_id, tag_order),
		FOREIGN KEY (event_id) REFERENCES event(id) ON DELETE CASCADE)`); err != nil {
		return fmt.Errorf("failed to create tag table: %w", err)
	}

	// Migrate existing tag data BEFORE creating indexes (much faster)
	// Foreign keys are disabled so no FK checks during insert
	if err := b.migrateExistingTags(); err != nil {
		return fmt.Errorf("failed to migrate existing tags: %w", err)
	}

	// Create indexes after data is migrated (more efficient)
	fmt.Println("  Creating index tag_identifier_idx...")
	if _, err := b.DB.Exec(`CREATE INDEX tag_identifier_idx ON tag(identifier)`); err != nil {
		return fmt.Errorf("failed to create tag_identifier_idx: %w", err)
	}

	fmt.Println("  Creating index tag_event_id_idx...")
	if _, err := b.DB.Exec(`CREATE INDEX tag_event_id_idx ON tag(event_id)`); err != nil {
		return fmt.Errorf("failed to create tag_event_id_idx: %w", err)
	}

	fmt.Println("  Creating composite index tag_identifier_first_data_idx...")
	if _, err := b.DB.Exec(`CREATE INDEX tag_identifier_first_data_idx ON tag(identifier, first_data)`); err != nil {
		return fmt.Errorf("failed to create tag_identifier_first_data_idx: %w", err)
	}

	// Re-enable foreign key constraints after migration is complete
	fmt.Println("  Re-enabling foreign key constraints...")
	if _, err := b.DB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	fmt.Println("Migration 2 complete")
	return nil
}

// tagInsert represents a tag to be inserted during migration
type tagInsert struct {
	eventID    string
	tagOrder   int
	identifier string
	firstData  string
	tagData    []byte
}

// migrateExistingTags migrates tag data from event.tags JSONB column to tag table
func (b *SQLite3Backend) migrateExistingTags() error {
	fmt.Println("  Migrating existing tags from event.tags to tag table...")

	// Query all events with their tags
	rows, err := b.DB.Query("SELECT id, tags FROM event")
	if err != nil {
		return fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	// Process each event and collect tags for batch insert
	migratedEvents := 0
	migratedTags := 0
	batchSize := 1000

	var tagBatch []tagInsert

	for rows.Next() {
		var eventID string
		var tagsJSON []byte

		if err := rows.Scan(&eventID, &tagsJSON); err != nil {
			return fmt.Errorf("failed to scan event row: %w", err)
		}

		// Parse tags JSON
		var tags [][]string
		if err := json.Unmarshal(tagsJSON, &tags); err != nil {
			// Skip events with invalid tag JSON
			fmt.Printf("    Warning: failed to parse tags for event %s: %v\n", eventID, err)
			continue
		}

		// Collect tags for this event
		for i, tag := range tags {
			if len(tag) == 0 {
				continue // Skip empty tags
			}

			identifier := tag[0]
			var firstData string
			if len(tag) > 1 {
				firstData = tag[1]
			}
			tagDataJSON, _ := json.Marshal(tag)

			tagBatch = append(tagBatch, tagInsert{
				eventID:    eventID,
				tagOrder:   i,
				identifier: identifier,
				firstData:  firstData,
				tagData:    tagDataJSON,
			})
		}

		migratedEvents++

		// Insert batch every 1000 events
		if migratedEvents%batchSize == 0 && len(tagBatch) > 0 {
			if err := b.insertTagBatch(tagBatch); err != nil {
				return fmt.Errorf("failed to insert tag batch: %w", err)
			}
			migratedTags += len(tagBatch)
			fmt.Printf("    Migrated tags for %d events (%d tags total)...\n", migratedEvents, migratedTags)
			tagBatch = tagBatch[:0] // Clear batch
		}
	}

	// Insert remaining tags
	if len(tagBatch) > 0 {
		if err := b.insertTagBatch(tagBatch); err != nil {
			return fmt.Errorf("failed to insert final tag batch: %w", err)
		}
		migratedTags += len(tagBatch)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating events: %w", err)
	}

	fmt.Printf("    Migration complete: processed %d events, created %d tag entries\n", migratedEvents, migratedTags)
	return nil
}

// insertTagBatch inserts a batch of tags in a single transaction
func (b *SQLite3Backend) insertTagBatch(tags []tagInsert) error {
	if len(tags) == 0 {
		return nil
	}

	// Begin transaction
	tx, err := b.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare insert statement
	stmt, err := tx.Prepare(`
		INSERT INTO tag (event_id, tag_order, identifier, first_data, tag_data)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Insert all tags in the batch
	for _, tag := range tags {
		if _, err := stmt.Exec(tag.eventID, tag.tagOrder, tag.identifier, tag.firstData, tag.tagData); err != nil {
			return fmt.Errorf("failed to insert tag for event %s: %w", tag.eventID, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// migration3 runs VACUUM and ANALYZE to optimize the database
func (b *SQLite3Backend) migration3() error {
	fmt.Println("Running migration 3...")

	// Execute ANALYZE before VACUUM to make VACUUM faster
	fmt.Println("  Running ANALYZE (pre-vacuum) to update statistics...")
	if _, err := b.DB.Exec("ANALYZE"); err != nil {
		return fmt.Errorf("failed to execute ANALYZE (pre-vacuum): %w", err)
	}

	// Execute VACUUM to rebuild the database, reclaim unused space, and defragment
	fmt.Println("  Running VACUUM to rebuild database and reclaim space...")
	if _, err := b.DB.Exec("VACUUM"); err != nil {
		return fmt.Errorf("failed to execute VACUUM: %w", err)
	}

	// Execute ANALYZE after VACUUM to update query planner statistics for the rebuilt indexes
	fmt.Println("  Running ANALYZE (post-vacuum) to update statistics for rebuilt indexes...")
	if _, err := b.DB.Exec("ANALYZE"); err != nil {
		return fmt.Errorf("failed to execute ANALYZE (post-vacuum): %w", err)
	}

	fmt.Println("Migration 3 complete")
	return nil
}
