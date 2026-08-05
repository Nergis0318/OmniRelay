package service

import (
	"path/filepath"
	"testing"

	"omnirelay/internal/config"
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
