package service

import (
	"path/filepath"
	"testing"
	"time"

	"omnirelay/internal/database"
	"omnirelay/internal/models"
)

func TestLogInsertsWithTimestamps(t *testing.T) {
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id) VALUES (1, 'p', 'P', 'http://x', 'enc', 'openai', 1)`); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	svc := NewUsageService(db)
	now := time.Now()
	apiKeyID := int64(1)
	providerID := int64(1)
	userID := int64(1)

	if err := svc.Log(models.UsageLog{
		APIKeyID:       &apiKeyID,
		ProviderID:     &providerID,
		UserID:         &userID,
		Model:          "p/m",
		RequestTokens:  10,
		ResponseTokens: 5,
		TotalTokens:    15,
		LatencyMs:      100,
		StartedAt:      &now,
		CompletedAt:    &now,
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE user_id = 1").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestLogResolvesZeroUserIDFromAPIKey(t *testing.T) {
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id) VALUES (1, 'p', 'P', 'http://x', 'enc', 'openai', 1)`); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	svc := NewUsageService(db)
	apiKeyID := int64(1)
	providerID := int64(1)
	userID := int64(0)

	if err := svc.Log(models.UsageLog{
		APIKeyID:   &apiKeyID,
		ProviderID: &providerID,
		UserID:     &userID,
		Model:      "p/m",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	logs, total, err := svc.Query(models.UsageQueryParams{}, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("resolved user_id should be visible to owner: total=%d len=%d", total, len(logs))
	}
	if logs[0].UserID == nil || *logs[0].UserID != 1 {
		t.Fatalf("resolved user_id = %v, want 1", logs[0].UserID)
	}
}