package database

import (
	"database/sql"
	"log"
)

func runMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			is_admin BOOLEAN DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_key TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			api_base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			provider_type TEXT NOT NULL CHECK(provider_type IN ('openai', 'anthropic', 'lmstudio', 'ollama', 'gemini')),
			is_active BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER REFERENCES providers(id) ON DELETE CASCADE,
			model_id TEXT NOT NULL,
			display_name TEXT,
			provider_key TEXT NOT NULL,
			is_manual BOOLEAN DEFAULT 0,
			input_price_per_1k REAL DEFAULT 0.0,
			output_price_per_1k REAL DEFAULT 0.0,
			context_window INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider_id, model_id)
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_hash TEXT UNIQUE NOT NULL,
			key_prefix TEXT NOT NULL,
			name TEXT NOT NULL,
			created_by INTEGER REFERENCES users(id),
			is_active BOOLEAN DEFAULT 1,
			rate_limit_rpm INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			api_key_id INTEGER REFERENCES api_keys(id),
			provider_id INTEGER REFERENCES providers(id),
			model TEXT NOT NULL,
			request_tokens INTEGER DEFAULT 0,
			response_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			latency_ms INTEGER DEFAULT 0,
			cost REAL DEFAULT 0.0,
			is_error BOOLEAN DEFAULT 0,
			error_message TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_models_provider_key ON models(provider_key)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_api_key_id ON usage_logs(api_key_id)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_created_at ON usage_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_provider_id ON usage_logs(provider_id)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return err
		}
	}

	addColumnIfMissing(db, "models", "input_price_per_1mtok", "REAL DEFAULT 0.0")
	addColumnIfMissing(db, "models", "output_price_per_1mtok", "REAL DEFAULT 0.0")
	addColumnIfMissing(db, "models", "cache_write_5m_price_per_1mtok", "REAL DEFAULT 0.0")
	addColumnIfMissing(db, "models", "cache_write_1h_price_per_1mtok", "REAL DEFAULT 0.0")
	addColumnIfMissing(db, "models", "cache_read_price_per_1mtok", "REAL DEFAULT 0.0")
	addColumnIfMissing(db, "providers", "user_id", "INTEGER REFERENCES users(id)")
	addColumnIfMissing(db, "models", "user_id", "INTEGER REFERENCES users(id)")
	addColumnIfMissing(db, "usage_logs", "user_id", "INTEGER REFERENCES users(id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_providers_user_id ON providers(user_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_models_user_id ON models(user_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_usage_user_id ON usage_logs(user_id)")

	migrateColumnIfExists(db, "models", "input_price_per_1k", "input_price_per_1mtok")
	migrateColumnIfExists(db, "models", "output_price_per_1k", "output_price_per_1mtok")
	migrateColumnIfExists(db, "models", "cache_write_5m_price_per_1k", "cache_write_5m_price_per_1mtok")
	migrateColumnIfExists(db, "models", "cache_write_1h_price_per_1k", "cache_write_1h_price_per_1mtok")
	migrateColumnIfExists(db, "models", "cache_hit_price_per_1k", "cache_read_price_per_1mtok")

	upgradeProviderTypeConstraint(db)

	log.Println("database migrations completed")
	return nil
}

func migrateColumnIfExists(db *sql.DB, table, oldCol, newCol string) {
	if !columnExists(db, table, oldCol) {
		return
	}
	db.Exec("UPDATE " + table + " SET " + newCol + " = " + oldCol + " WHERE " + newCol + " = 0 AND " + oldCol + " != 0")
}

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue interface{}
		var pk int
		rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk)
		if name == column {
			return true
		}
	}
	return false
}

func addColumnIfMissing(db *sql.DB, table, column, colType string) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue interface{}
		var pk int
		rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk)
		if name == column {
			return
		}
	}

	db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + colType)
}

func upgradeProviderTypeConstraint(db *sql.DB) {
	rows, err := db.Query("SELECT sql FROM sqlite_master WHERE type='table' AND name='providers'")
	if err != nil {
		return
	}
	defer rows.Close()

	var ddl string
	if rows.Next() {
		rows.Scan(&ddl)
	}

	if ddl == "" || containsAny(ddl, "gemini") {
		return
	}

	log.Println("upgrading provider_type constraint to include lmstudio, ollama...")

	db.Exec("PRAGMA foreign_keys=off")
	tx, err := db.Begin()
	if err != nil {
		return
	}

	queries := []string{
		`CREATE TABLE providers_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_key TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			api_base_url TEXT NOT NULL,
			api_key_encrypted TEXT NOT NULL,
			provider_type TEXT NOT NULL CHECK(provider_type IN ('openai', 'anthropic', 'lmstudio', 'ollama', 'gemini')),
			is_active BOOLEAN DEFAULT 1,
			user_id INTEGER REFERENCES users(id),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO providers_new SELECT * FROM providers`,
		`DROP TABLE providers`,
		`ALTER TABLE providers_new RENAME TO providers`,
		`CREATE INDEX IF NOT EXISTS idx_models_provider_key ON models(provider_key)`,
		`CREATE INDEX IF NOT EXISTS idx_providers_user_id ON providers(user_id)`,
	}

	for _, q := range queries {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			db.Exec("PRAGMA foreign_keys=on")
			return
		}
	}

	tx.Commit()
	db.Exec("PRAGMA foreign_keys=on")
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
