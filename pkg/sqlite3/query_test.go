package sqlite3

import (
	"database/sql"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nbd-wtf/go-nostr"
)

func TestQueryEventsSql_Kind1984WithManyAuthorsAndTags(t *testing.T) {
	backend := &SQLite3Backend{
		QueryLimit:        100,
		QueryIDsLimit:     500,
		QueryAuthorsLimit: 500,
		QueryKindsLimit:   10,
		QueryTagsLimit:    10,
	}

	// This is the slow query from the logs
	filter := nostr.Filter{
		Kinds: []int{1984},
		Authors: []string{
			"58c741aa630c2da35a56a77c1d05381908bd10504fdd2d8b43f725efa6d23196",
			"7644119b18ec56b5b2779e0d035e7712a9d669dbfbe5b2c5f458cb564afb6c95",
			"863f2c555276e9ed738933b0efee6b021042f16e1529dd755704885b87fee183",
			"c9f4f6847f2c8c76964e37a2c26f2d3a153f9d4e7231e5705c8952b6345c292a",
			"f275ab37d64f6be0379a85ce5736060e164b136a08c7f1e78dea633abad84bfe",
		},
		Tags: nostr.TagMap{
			"p": []string{
				"35d2cbc223ef181b9368048a8117b8b55b1ef676b78170786203970c6e0d079a",
				"6d5a6d6c148ac36b07bb277a6f711faca177675a2f3dc0fedd73d39b473d15c6",
				"802b590f4e6d3c0329a6c4baef0b0bae33301f2879e1b3111488c5c8a493697e",
				"8230c6222dea501c168d871de40d3ced4946b5608683af486a22e55426642641",
			},
		},
		Since: func() *nostr.Timestamp { ts := nostr.Timestamp(1759969515); return &ts }(),
	}

	query, params, err := backend.queryEventsSql(filter, false)
	if err != nil {
		t.Fatalf("queryEventsSql failed: %v", err)
	}

	t.Logf("Generated SQL:\n%s\n", query)
	t.Logf("Parameters (%d total):\n", len(params))
	for i, param := range params {
		t.Logf("  [%d] = %v", i, param)
	}

	// Also test COUNT query
	countQuery, countParams, err := backend.queryEventsSql(filter, true)
	if err != nil {
		t.Fatalf("queryEventsSql (count) failed: %v", err)
	}

	t.Logf("\nGenerated COUNT SQL:\n%s\n", countQuery)
	t.Logf("COUNT Parameters (%d total):\n", len(countParams))
	for i, param := range countParams {
		t.Logf("  [%d] = %v", i, param)
	}
}

func TestQueryEventsSql_SimplifiedKind1984(t *testing.T) {
	backend := &SQLite3Backend{
		QueryLimit:        100,
		QueryIDsLimit:     500,
		QueryAuthorsLimit: 500,
		QueryKindsLimit:   10,
		QueryTagsLimit:    10,
	}

	// Test with just kind and authors (no tags)
	filter := nostr.Filter{
		Kinds: []int{1984},
		Authors: []string{
			"58c741aa630c2da35a56a77c1d05381908bd10504fdd2d8b43f725efa6d23196",
			"7644119b18ec56b5b2779e0d035e7712a9d669dbfbe5b2c5f458cb564afb6c95",
		},
		Since: func() *nostr.Timestamp { ts := nostr.Timestamp(1759969515); return &ts }(),
	}

	query, params, err := backend.queryEventsSql(filter, false)
	if err != nil {
		t.Fatalf("queryEventsSql failed: %v", err)
	}

	t.Logf("Simplified SQL (no tags):\n%s\n", query)
	t.Logf("Parameters (%d total):\n", len(params))
	for i, param := range params {
		t.Logf("  [%d] = %v", i, param)
	}
}

func TestQueryPlan_Kind1984WithManyAuthorsAndTags(t *testing.T) {
	// Load .env file from project root
	if err := godotenv.Load("../../.env"); err != nil {
		t.Logf("Warning: Could not load .env file: %v", err)
	}

	// Get database path from environment
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		t.Skip("DB_PATH not set in .env file, skipping test")
	}

	// If path is relative, make it relative to project root
	if dbPath[0] != '/' {
		dbPath = "../../" + dbPath
	}

	// Connect to the real database
	db, err := sqlx.Connect("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database %s: %v", dbPath, err)
	}
	defer db.Close()

	db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)

	// Create backend (no migrations needed, using existing DB)
	backend := &SQLite3Backend{
		DatabaseURL:       dbPath,
		QueryLimit:        100,
		QueryIDsLimit:     500,
		QueryAuthorsLimit: 500,
		QueryKindsLimit:   10,
		QueryTagsLimit:    10,
	}
	backend.DB = db

	// The slow query filter
	filter := nostr.Filter{
		Kinds: []int{1984},
		Authors: []string{
			"58c741aa630c2da35a56a77c1d05381908bd10504fdd2d8b43f725efa6d23196",
			"7644119b18ec56b5b2779e0d035e7712a9d669dbfbe5b2c5f458cb564afb6c95",
			"863f2c555276e9ed738933b0efee6b021042f16e1529dd755704885b87fee183",
			"c9f4f6847f2c8c76964e37a2c26f2d3a153f9d4e7231e5705c8952b6345c292a",
			"f275ab37d64f6be0379a85ce5736060e164b136a08c7f1e78dea633abad84bfe",
		},
		Tags: nostr.TagMap{
			"p": []string{
				"35d2cbc223ef181b9368048a8117b8b55b1ef676b78170786203970c6e0d079a",
				"6d5a6d6c148ac36b07bb277a6f711faca177675a2f3dc0fedd73d39b473d15c6",
				"802b590f4e6d3c0329a6c4baef0b0bae33301f2879e1b3111488c5c8a493697e",
				"8230c6222dea501c168d871de40d3ced4946b5608683af486a22e55426642641",
			},
		},
		Since: func() *nostr.Timestamp { ts := nostr.Timestamp(1759969515); return &ts }(),
	}

	query, params, err := backend.queryEventsSql(filter, false)
	if err != nil {
		t.Fatalf("queryEventsSql failed: %v", err)
	}

	t.Logf("\n=== QUERY ===\n%s\n", query)

	// Show the query plan
	explainQuery := "EXPLAIN QUERY PLAN " + query
	rows, err := db.Query(explainQuery, params...)
	if err != nil {
		t.Fatalf("Failed to get query plan: %v", err)
	}
	defer rows.Close()

	t.Logf("\n=== QUERY PLAN ===")
	var id, parent, notused int
	var detail string
	for rows.Next() {
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("Failed to scan query plan: %v", err)
		}
		indent := ""
		for i := 0; i < parent; i++ {
			indent += "  "
		}
		t.Logf("%s%s", indent, detail)
	}

	// List all indexes on event table
	t.Logf("\n=== INDEXES ON event TABLE ===")
	indexRows, err := db.Query(`
		SELECT name, sql FROM sqlite_master 
		WHERE type='index' AND tbl_name='event' 
		ORDER BY name
	`)
	if err != nil {
		t.Fatalf("Failed to list indexes: %v", err)
	}
	defer indexRows.Close()

	for indexRows.Next() {
		var name string
		var sql sql.NullString
		if err := indexRows.Scan(&name, &sql); err != nil {
			t.Fatalf("Failed to scan index: %v", err)
		}
		if sql.Valid {
			t.Logf("%s: %s", name, sql.String)
		} else {
			t.Logf("%s: (auto-created)", name)
		}
	}

	// List all indexes on tag table
	t.Logf("\n=== INDEXES ON tag TABLE ===")
	tagIndexRows, err := db.Query(`
		SELECT name, sql FROM sqlite_master 
		WHERE type='index' AND tbl_name='tag' 
		ORDER BY name
	`)
	if err != nil {
		t.Fatalf("Failed to list tag indexes: %v", err)
	}
	defer tagIndexRows.Close()

	for tagIndexRows.Next() {
		var name string
		var sql sql.NullString
		if err := tagIndexRows.Scan(&name, &sql); err != nil {
			t.Fatalf("Failed to scan tag index: %v", err)
		}
		if sql.Valid {
			t.Logf("%s: %s", name, sql.String)
		} else {
			t.Logf("%s: (auto-created)", name)
		}
	}
}

func TestQueryPlan_SimplifiedKind1984(t *testing.T) {
	// Load .env file from project root
	if err := godotenv.Load("../../.env"); err != nil {
		t.Logf("Warning: Could not load .env file: %v", err)
	}

	// Get database path from environment
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		t.Skip("DB_PATH not set in .env file, skipping test")
	}

	// If path is relative, make it relative to project root
	if dbPath[0] != '/' {
		dbPath = "../../" + dbPath
	}

	// Connect to the real database
	db, err := sqlx.Connect("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database %s: %v", dbPath, err)
	}
	defer db.Close()

	db.Mapper = reflectx.NewMapperFunc("json", sqlx.NameMapper)

	// Create backend (no migrations needed, using existing DB)
	backend := &SQLite3Backend{
		DatabaseURL:       dbPath,
		QueryLimit:        100,
		QueryIDsLimit:     500,
		QueryAuthorsLimit: 500,
		QueryKindsLimit:   10,
		QueryTagsLimit:    10,
	}
	backend.DB = db

	// Query without tags (should be much faster)
	filter := nostr.Filter{
		Kinds: []int{1984},
		Authors: []string{
			"58c741aa630c2da35a56a77c1d05381908bd10504fdd2d8b43f725efa6d23196",
			"7644119b18ec56b5b2779e0d035e7712a9d669dbfbe5b2c5f458cb564afb6c95",
		},
		Since: func() *nostr.Timestamp { ts := nostr.Timestamp(1759969515); return &ts }(),
	}

	query, params, err := backend.queryEventsSql(filter, false)
	if err != nil {
		t.Fatalf("queryEventsSql failed: %v", err)
	}

	t.Logf("\n=== QUERY (no tags) ===\n%s\n", query)

	// Show the query plan
	explainQuery := "EXPLAIN QUERY PLAN " + query
	rows, err := db.Query(explainQuery, params...)
	if err != nil {
		t.Fatalf("Failed to get query plan: %v", err)
	}
	defer rows.Close()

	t.Logf("\n=== QUERY PLAN (no tags) ===")
	var id, parent, notused int
	var detail string
	for rows.Next() {
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("Failed to scan query plan: %v", err)
		}
		indent := ""
		for i := 0; i < parent; i++ {
			indent += "  "
		}
		t.Logf("%s%s", indent, detail)
	}
}
