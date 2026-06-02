package service

import (
	"database/sql"
	"path/filepath"
	"testing"

	"omnirelay/internal/database"
)

func newTestModelService(t *testing.T) *ModelService {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewModelService(db)
}

func seedProvider(t *testing.T, db *sql.DB, key string, userID *int64) int64 {
	t.Helper()
	var id int64
	if userID == nil {
		err := db.QueryRow(
			`INSERT INTO providers (provider_key, name, provider_type, api_base_url, api_key_encrypted, is_active, user_id)
			 VALUES (?, ?, 'openai', 'https://api.example.com', 'enc', 1, NULL) RETURNING id`,
			key, key,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert provider: %v", err)
		}
		return id
	}
	err := db.QueryRow(
		`INSERT INTO providers (provider_key, name, provider_type, api_base_url, api_key_encrypted, is_active, user_id)
		 VALUES (?, ?, 'openai', 'https://api.example.com', 'enc', 1, ?) RETURNING id`,
		key, key, *userID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	return id
}

func TestCountActiveScopesByUser(t *testing.T) {
	svc := newTestModelService(t)
	db := svc.db

	pShared := seedProvider(t, db, "shared", nil)
	pUser1 := seedProvider(t, db, "u1", int64Ptr(1))

	_, err := db.Exec(
		`INSERT INTO models (provider_id, model_id, provider_key, is_manual, user_id) VALUES (?, 'm-shared', 'shared', 1, NULL)`,
		pShared,
	)
	if err != nil {
		t.Fatalf("insert shared model: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO models (provider_id, model_id, provider_key, is_manual, user_id) VALUES (?, 'm-u1', 'u1', 1, 1)`,
		pUser1,
	)
	if err != nil {
		t.Fatalf("insert user model: %v", err)
	}

	count, err := svc.CountActive(1)
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if count != 2 {
		t.Errorf("user 1 count = %d, want 2 (shared + own)", count)
	}

	count, err = svc.CountActive(2)
	if err != nil {
		t.Fatalf("CountActive user 2: %v", err)
	}
	if count != 1 {
		t.Errorf("user 2 count = %d, want 1 (shared only)", count)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}