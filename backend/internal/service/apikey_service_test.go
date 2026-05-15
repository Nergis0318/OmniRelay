package service

import (
	"errors"
	"path/filepath"
	"testing"

	"omnirelay/internal/database"
	"omnirelay/internal/models"
)

func newTestAPIKeyService(t *testing.T) *APIKeyService {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewAPIKeyService(db)
}

func TestValidateEnforcesRateLimit(t *testing.T) {
	svc := newTestAPIKeyService(t)

	created, err := svc.Create(models.CreateAPIKeyRequest{Name: "limited", RateLimitRPM: 1}, 1)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if _, err := svc.Validate(created.PlainKey); err != nil {
		t.Fatalf("validate before usage: %v", err)
	}

	_, err = svc.db.Exec(
		"INSERT INTO usage_logs (api_key_id, model, user_id) VALUES (?, ?, ?)",
		created.APIKey.ID, "openai/test", 1,
	)
	if err != nil {
		t.Fatalf("insert usage log: %v", err)
	}

	_, err = svc.Validate(created.PlainKey)
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("expected rate limit error, got %v", err)
	}
}
