package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunMigrationsFreshDatabase(t *testing.T) {
	db := openTestDB(t)

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations (fresh): %v", err)
	}

	// Verify schema_migrations has all versions
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != len(migrations) {
		t.Errorf("expected %d migrations, got %d", len(migrations), count)
	}

	// Verify tables exist
	tables := []string{"users", "providers", "models", "api_keys", "usage_logs"}
	for _, table := range tables {
		var exists int
		db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists)
		if exists == 0 {
			t.Errorf("table %s should exist", table)
		}
	}
}

func TestRunMigrationsIdempotent(t *testing.T) {
	db := openTestDB(t)

	if err := runMigrations(db); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run should not error
	if err := runMigrations(db); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// Verify counts still correct
	var count int
	db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if count != len(migrations) {
		t.Errorf("expected %d migrations after second run, got %d", len(migrations), count)
	}
}

func TestMigrationsCreateTables(t *testing.T) {
	db := openTestDB(t)
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	// Verify columns exist from migration v7 (api_keys.total_token_limit)
	rows, err := db.Query("PRAGMA table_info(api_keys)")
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close()

	foundTokenLimit := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "total_token_limit" {
			foundTokenLimit = true
		}
	}
	if !foundTokenLimit {
		t.Error("expected total_token_limit column in api_keys (migration v7)")
	}
}

func TestMigrationV12CreatesProviderEndpoints(t *testing.T) {
	db := openTestDB(t)
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	rows, err := db.Query(`SELECT name FROM pragma_table_info('provider_endpoints')`)
	if err != nil {
		t.Fatalf("query pragma: %v", err)
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	for _, want := range []string{"provider_id", "api_type", "base_url"} {
		if !cols[want] {
			t.Errorf("provider_endpoints missing column %q (got %v)", want, cols)
		}
	}
}

func TestHasColumn(t *testing.T) {
	db := openTestDB(t)
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	// Existing column
	exists, err := hasColumn(tx, "api_keys", "id")
	if err != nil {
		t.Fatalf("hasColumn(id): %v", err)
	}
	if !exists {
		t.Error("expected id column to exist")
	}

	// Non-existing column
	exists, err = hasColumn(tx, "api_keys", "nonexistent")
	if err != nil {
		t.Fatalf("hasColumn(nonexistent): %v", err)
	}
	if exists {
		t.Error("expected nonexistent column to not exist")
	}
}
