package service

import (
	"errors"
	"path/filepath"
	"strings"
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

func TestCreateReturnsPlainKeyAndPrefix(t *testing.T) {
	svc := newTestAPIKeyService(t)

	created, err := svc.Create(models.CreateAPIKeyRequest{Name: "test", RateLimitRPM: 0}, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(created.PlainKey, "om-ni-") {
		t.Errorf("plain key should start with om-ni-, got %q", created.PlainKey)
	}
	if len(created.PlainKey) != len("om-ni-")+64 { // 32 bytes hex-encoded
		t.Errorf("plain key length = %d, want %d", len(created.PlainKey), len("om-ni-")+64)
	}
	if created.APIKey.KeyPrefix != created.PlainKey[:17] {
		t.Errorf("prefix mismatch: stored %q vs first 17 chars of plain %q", created.APIKey.KeyPrefix, created.PlainKey[:17])
	}
	if !created.APIKey.IsActive {
		t.Error("newly created key should be active")
	}
}

func TestValidateRejectsUnknownKey(t *testing.T) {
	svc := newTestAPIKeyService(t)
	if _, err := svc.Validate("om-ni-doesnotexist"); err == nil || !strings.Contains(err.Error(), "invalid API key") {
		t.Fatalf("expected \"invalid API key\", got %v", err)
	}
}

func TestValidateRejectsInactiveKey(t *testing.T) {
	svc := newTestAPIKeyService(t)
	created, err := svc.Create(models.CreateAPIKeyRequest{Name: "to-delete", RateLimitRPM: 0}, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Delete(created.APIKey.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Validate(created.PlainKey); err == nil || !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("expected inactive error, got %v", err)
	}
}

func TestValidateWithZeroRateLimitIsUnlimited(t *testing.T) {
	svc := newTestAPIKeyService(t)
	created, err := svc.Create(models.CreateAPIKeyRequest{Name: "unlimited", RateLimitRPM: 0}, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Stuff a bunch of usage rows; Validate should still succeed when rate_limit_rpm <= 0.
	for i := 0; i < 50; i++ {
		if _, err := svc.db.Exec(
			"INSERT INTO usage_logs (api_key_id, model, user_id) VALUES (?, ?, ?)",
			created.APIKey.ID, "openai/test", 1,
		); err != nil {
			t.Fatalf("insert usage log: %v", err)
		}
	}
	if _, err := svc.Validate(created.PlainKey); err != nil {
		t.Fatalf("validate with rate_limit_rpm=0 should pass, got %v", err)
	}
}

func TestListReturnsKeysForOwnerOnly(t *testing.T) {
	svc := newTestAPIKeyService(t)
	if _, err := svc.Create(models.CreateAPIKeyRequest{Name: "owner-a-key1"}, 1); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	if _, err := svc.Create(models.CreateAPIKeyRequest{Name: "owner-a-key2"}, 1); err != nil {
		t.Fatalf("create a2: %v", err)
	}
	if _, err := svc.Create(models.CreateAPIKeyRequest{Name: "owner-b-key1"}, 2); err != nil {
		t.Fatalf("create b1: %v", err)
	}

	aKeys, err := svc.List(1)
	if err != nil {
		t.Fatalf("list 1: %v", err)
	}
	if len(aKeys) != 2 {
		t.Errorf("user 1 should see 2 keys, got %d", len(aKeys))
	}
	for _, k := range aKeys {
		if k.CreatedBy != 1 {
			t.Errorf("List(1) returned key with CreatedBy=%d", k.CreatedBy)
		}
	}

	bKeys, err := svc.List(2)
	if err != nil {
		t.Fatalf("list 2: %v", err)
	}
	if len(bKeys) != 1 {
		t.Errorf("user 2 should see 1 key, got %d", len(bKeys))
	}
}

func TestCountActiveReflectsDeletes(t *testing.T) {
	svc := newTestAPIKeyService(t)
	a, _ := svc.Create(models.CreateAPIKeyRequest{Name: "a"}, 1)
	_, _ = svc.Create(models.CreateAPIKeyRequest{Name: "b"}, 1)
	count, err := svc.CountActive()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	if err := svc.Delete(a.APIKey.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	count, _ = svc.CountActive()
	if count != 1 {
		t.Errorf("after delete count = %d, want 1", count)
	}
}
