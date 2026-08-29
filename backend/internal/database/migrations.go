package database

import (
	"database/sql"
	"log"
)

type migration struct {
	version int
	up      func(*sql.Tx) error
}

var migrations = []migration{
	{
		version: 1,
		up: func(tx *sql.Tx) error {
			stmts := []string{
				`CREATE TABLE IF NOT EXISTS users (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					username TEXT NOT NULL,
					email TEXT NOT NULL DEFAULT '',
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
					user_id INTEGER REFERENCES users(id),
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
					input_price_per_1mtok REAL DEFAULT 0.0,
					output_price_per_1mtok REAL DEFAULT 0.0,
					cache_write_5m_price_per_1mtok REAL DEFAULT 0.0,
					cache_write_1h_price_per_1mtok REAL DEFAULT 0.0,
					cache_read_price_per_1mtok REAL DEFAULT 0.0,
					context_window INTEGER DEFAULT 0,
					user_id INTEGER REFERENCES users(id),
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
					user_id INTEGER REFERENCES users(id),
					cache_write_5m_tokens INTEGER DEFAULT 0,
					cache_write_1h_tokens INTEGER DEFAULT 0,
					cache_read_tokens INTEGER DEFAULT 0,
					started_at DATETIME,
					completed_at DATETIME,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)`,
				`CREATE INDEX IF NOT EXISTS idx_models_provider_key ON models(provider_key)`,
				`CREATE INDEX IF NOT EXISTS idx_usage_api_key_id ON usage_logs(api_key_id)`,
				`CREATE INDEX IF NOT EXISTS idx_usage_created_at ON usage_logs(created_at)`,
				`CREATE INDEX IF NOT EXISTS idx_usage_provider_id ON usage_logs(provider_id)`,
				`CREATE INDEX IF NOT EXISTS idx_providers_user_id ON providers(user_id)`,
				`CREATE INDEX IF NOT EXISTS idx_models_user_id ON models(user_id)`,
				`CREATE INDEX IF NOT EXISTS idx_usage_user_id ON usage_logs(user_id)`,
			}
			for _, s := range stmts {
				if _, err := tx.Exec(s); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 2,
		up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
				version INTEGER PRIMARY KEY
			)`); err != nil {
				return err
			}
			return nil
		},
	},
	{
		version: 3,
		up: func(tx *sql.Tx) error {
			hasColumn, err := hasColumn(tx, "users", "email")
			if err != nil {
				return err
			}
			if !hasColumn {
				if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''`); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email <> ''`); err != nil {
				return err
			}
			return nil
		},
	},
	{
		version: 4,
		up: func(tx *sql.Tx) error {
			hasColumn, err := hasColumn(tx, "providers", "user_id")
			if err != nil {
				return err
			}
			if !hasColumn {
				if _, err := tx.Exec(`ALTER TABLE providers ADD COLUMN user_id INTEGER REFERENCES users(id)`); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_providers_user_id ON providers(user_id)`); err != nil {
				return err
			}
			return nil
		},
	},
	{
		version: 5,
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				UPDATE usage_logs
				SET user_id = (SELECT created_by FROM api_keys WHERE api_keys.id = usage_logs.api_key_id)
				WHERE user_id IS NULL AND api_key_id IS NOT NULL
				  AND EXISTS (SELECT 1 FROM api_keys WHERE api_keys.id = usage_logs.api_key_id AND created_by IS NOT NULL)
			`)
			return err
		},
	},
	{
		version: 6,
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				UPDATE usage_logs
				SET user_id = (SELECT created_by FROM api_keys WHERE api_keys.id = usage_logs.api_key_id)
				WHERE (user_id IS NULL OR user_id = 0) AND api_key_id IS NOT NULL
				  AND EXISTS (SELECT 1 FROM api_keys WHERE api_keys.id = usage_logs.api_key_id AND created_by IS NOT NULL)
			`)
			return err
		},
	},
	{
		version: 7,
		up: func(tx *sql.Tx) error {
			hasColumn, err := hasColumn(tx, "api_keys", "total_token_limit")
			if err != nil {
				return err
			}
			if !hasColumn {
				if _, err := tx.Exec(`ALTER TABLE api_keys ADD COLUMN total_token_limit INTEGER DEFAULT 0`); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 8,
		up: func(tx *sql.Tx) error {
			hasColumn, err := hasColumn(tx, "models", "source_provider_key")
			if err != nil {
				return err
			}
			if !hasColumn {
				if _, err := tx.Exec(`ALTER TABLE models ADD COLUMN source_provider_key TEXT NOT NULL DEFAULT ''`); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 9,
		up: func(tx *sql.Tx) error {
			hasColumn, err := hasColumn(tx, "providers", "show_in_model_list")
			if err != nil {
				return err
			}
			if !hasColumn {
				if _, err := tx.Exec(`ALTER TABLE providers ADD COLUMN show_in_model_list BOOLEAN DEFAULT 1`); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 10,
		up: func(tx *sql.Tx) error {
			// Add 'custom' to the provider_type CHECK constraint.
			// SQLite does not support ALTER TABLE for CHECK constraints,
			// so we recreate the table.
			if _, err := tx.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE TABLE providers_v10 (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				provider_key TEXT UNIQUE NOT NULL,
				name TEXT NOT NULL,
				api_base_url TEXT NOT NULL,
				api_key_encrypted TEXT NOT NULL,
				provider_type TEXT NOT NULL CHECK(provider_type IN ('openai', 'anthropic', 'lmstudio', 'ollama', 'gemini', 'custom')),
				is_active BOOLEAN DEFAULT 1,
				show_in_model_list BOOLEAN DEFAULT 1,
				user_id INTEGER REFERENCES users(id),
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO providers_v10 (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, show_in_model_list, user_id, created_at, updated_at)
				SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, COALESCE(show_in_model_list, 1), user_id, created_at, updated_at FROM providers`); err != nil {
				return err
			}
			if _, err := tx.Exec(`DROP TABLE providers`); err != nil {
				return err
			}
			if _, err := tx.Exec(`ALTER TABLE providers_v10 RENAME TO providers`); err != nil {
				return err
			}
			if _, err := tx.Exec(`PRAGMA foreign_keys=ON`); err != nil {
				return err
			}
			return nil
		},
	},
	{
		version: 11,
		up: func(tx *sql.Tx) error {
			stmts := []string{
				`CREATE TABLE IF NOT EXISTS password_reset_codes (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
					code TEXT UNIQUE NOT NULL,
					expires_at DATETIME NOT NULL,
					used BOOLEAN DEFAULT 0,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)`,
				`CREATE TABLE IF NOT EXISTS user_providers (
					user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
					provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
					PRIMARY KEY (user_id, provider_id)
				)`,
			}
			for _, s := range stmts {
				if _, err := tx.Exec(s); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 12,
		up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS provider_endpoints (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
				api_type TEXT NOT NULL,
				base_url TEXT NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(provider_id, api_type)
			)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_provider_endpoints_provider_id ON provider_endpoints(provider_id)`); err != nil {
				return err
			}
			return nil
		},
	},
	{
		version: 13,
		up: func(tx *sql.Tx) error {
			hasColumn, err := hasColumn(tx, "usage_logs", "ttft_ms")
			if err != nil {
				return err
			}
			if !hasColumn {
				if _, err := tx.Exec(`ALTER TABLE usage_logs ADD COLUMN ttft_ms INTEGER DEFAULT NULL`); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_ttft ON usage_logs(ttft_ms) WHERE ttft_ms IS NOT NULL`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_perf ON usage_logs(user_id, created_at, provider_id)`); err != nil {
				return err
			}
			return nil
		},
	},
}

func hasColumn(tx *sql.Tx, tableName, columnName string) (bool, error) {
	rows, err := tx.Query(`PRAGMA table_info(` + tableName + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var dfltValue *string
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY
	)`); err != nil {
		return err
	}

	var currentVersion int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		if err := m.up(tx); err != nil {
			tx.Rollback()
			return err
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		log.Printf("applied migration v%d", m.version)
	}

	log.Println("database migrations completed")
	return nil
}
