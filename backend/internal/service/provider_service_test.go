package service

import (
	"path/filepath"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/crypto"
	"omnirelay/internal/database"
	"omnirelay/internal/models"
)

func newTestProviderService(t *testing.T) *ProviderService {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES ('u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return NewProviderService(db, &config.Config{EncryptKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
}

func TestProviderEndpointsRoundTrip(t *testing.T) {
	svc := newTestProviderService(t)

	created, err := svc.Create(models.CreateProviderRequest{
		ProviderKey:  "gw",
		Name:         "Gateway",
		APiBaseURL:   "https://default.example/v1",
		APIKey:       "sk-1",
		ProviderType: "openai",
		Endpoints: []models.ProviderEndpoint{
			{APIType: "anthropic", BaseURL: "https://anthropic.example"},
			{APIType: "ollama", BaseURL: "http://ollama.local"},
		},
	}, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(created.Endpoints))
	}
	if created.Endpoints[0].APIType != "anthropic" || created.Endpoints[0].BaseURL != "https://anthropic.example" {
		t.Fatalf("endpoints[0] = %+v", created.Endpoints[0])
	}

	// GetByID loads endpoints back
	got, err := svc.GetByID(created.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Endpoints) != 2 {
		t.Fatalf("get endpoints = %d, want 2", len(got.Endpoints))
	}

	// Update replaces the endpoint set (delete + insert)
	upd := []models.ProviderEndpoint{{APIType: "gemini", BaseURL: "https://gemini.example"}}
	_, err = svc.Update(created.ID, 1, models.UpdateProviderRequest{Endpoints: &upd})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	final, _ := svc.GetByID(created.ID, 1)
	if len(final.Endpoints) != 1 || final.Endpoints[0].APIType != "gemini" {
		t.Fatalf("after update endpoints = %+v", final.Endpoints)
	}
}

func TestProviderEndpointsRejectsInvalidType(t *testing.T) {
	svc := newTestProviderService(t)
	_, err := svc.Create(models.CreateProviderRequest{
		ProviderKey:  "gw",
		Name:         "Gateway",
		APiBaseURL:   "https://default.example",
		APIKey:       "sk-1",
		ProviderType: "openai",
		Endpoints:    []models.ProviderEndpoint{{APIType: "bogus", BaseURL: "https://x.example"}},
	}, 1)
	if err == nil {
		t.Fatal("expected error for invalid api_type")
	}
}

func TestCreateProviderWritesChildKey(t *testing.T) {
	svc := newTestProviderService(t)
	p, err := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-abcdefghi", ProviderType: "openai",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.APIKeys) != 1 || p.APIKeys[0].KeyPrefix != "sk-abcde" || !p.APIKeys[0].IsActive {
		t.Fatalf("api_keys = %+v", p.APIKeys)
	}
	keys, err := svc.ListActiveKeys(p)
	if err != nil || len(keys) != 1 || keys[0].Plaintext != "sk-abcdefghi" {
		t.Fatalf("active = %+v err=%v", keys, err)
	}
}

func TestAddKeyAndRoundRobin(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "key-one-xx", ProviderType: "openai",
	}, 1)
	if _, err := svc.AddKey(p.ID, 1, "key-two-yy"); err != nil {
		t.Fatal(err)
	}
	p, _ = svc.GetByID(p.ID, 1)
	keys, err := svc.ListActiveKeys(p)
	if err != nil || len(keys) != 2 {
		t.Fatalf("want 2 keys, got %+v err=%v", keys, err)
	}
	i0 := svc.NextStartIndex(p.ID, 2)
	i1 := svc.NextStartIndex(p.ID, 2)
	if i0 == i1 {
		t.Fatalf("RR did not advance: %d %d", i0, i1)
	}
}

func TestDeleteLastActiveKeyRejected(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-1", ProviderType: "openai",
	}, 1)
	err := svc.DeleteKey(p.ID, p.APIKeys[0].ID, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ProviderError)
	if !ok || pe.StatusCode != 400 {
		t.Fatalf("got %v", err)
	}
}

func TestSetKeyActiveLastRejected(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-1", ProviderType: "openai",
	}, 1)
	err := svc.SetKeyActive(p.ID, p.APIKeys[0].ID, 1, false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListActiveKeysNoFallbackWhenInactiveChild(t *testing.T) {
	svc := newTestProviderService(t)
	p, err := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-parent", ProviderType: "openai",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeactivateKey(p.APIKeys[0].ID); err != nil {
		t.Fatal(err)
	}
	keys, err := svc.ListActiveKeys(p)
	if err != nil || len(keys) != 0 {
		t.Fatalf("want empty, got %+v err=%v", keys, err)
	}
}

func TestFallbackWhenNoChildRows(t *testing.T) {
	svc := newTestProviderService(t)
	enc := encryptForTest(t, svc, "sk-fallback")
	if _, err := svc.db.Exec(
		`INSERT INTO providers (provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES ('legacy', 'L', 'https://example/v1', ?, 'openai', 1)`, enc,
	); err != nil {
		t.Fatal(err)
	}
	p, err := svc.GetByKey("legacy", 1)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := svc.ListActiveKeys(p)
	if err != nil || len(keys) != 1 || keys[0].Plaintext != "sk-fallback" {
		t.Fatalf("fallback = %+v err=%v", keys, err)
	}
}

func TestUpdateAPIKeyInsertsAnother(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-one", ProviderType: "openai",
	}, 1)
	k2 := "sk-two"
	got, err := svc.Update(p.ID, 1, models.UpdateProviderRequest{APIKey: &k2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.APIKeys) != 2 {
		t.Fatalf("want 2 keys, got %+v", got.APIKeys)
	}
}

func encryptForTest(t *testing.T, svc *ProviderService, plain string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plain, svc.cfg.EncryptKey)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}
