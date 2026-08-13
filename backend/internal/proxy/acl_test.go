package proxy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

// seedACLDB seeds users, providers, models, and user_providers ACL rows for
// the path-routed ACL bypass test.
func seedACLDB(t *testing.T, upstreamURL string) (*service.ProviderService, *service.ModelService, *service.UsageService, *service.AuthService) {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'admin', 'h'), (2, 'victim', 'h')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	// Provider 1: openai, owned by admin — the *target* of the bypass.
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'secret-openai', 'Secret OpenAI', ?, ?, 'openai', 1)`,
		upstreamURL, encryptTestAPIKey(t, "sk-secret"),
	); err != nil {
		t.Fatalf("seed secret provider: %v", err)
	}
	// Provider 2: custom provider owned by user 2, sourcing models from provider 1.
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (2, 'my-custom', 'My Custom', '', '', 'custom', 2)`,
	); err != nil {
		t.Fatalf("seed custom provider: %v", err)
	}
	// Model under provider 2 that sources from the secret provider.
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, source_provider_key, user_id)
		 VALUES (1, 2, 'gpt-4o', 'my-custom', 0, 'secret-openai', 2)`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	// User 2 may access only the custom provider (provider 2); the secret
	// provider (1) is not in the allow list, so it must be denied.
	if _, err := db.Exec(`INSERT INTO user_providers (user_id, provider_id) VALUES (2, 2)`); err != nil {
		t.Fatalf("seed ACL: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 2)`); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, testConfig)
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	authSvc := service.NewAuthService(db)
	return providerSvc, modelSvc, usageSvc, authSvc
}

// TestPathRoutedSourceProviderACLEnforced ensures that when a path-routed
// request resolves a model whose source_provider_key points at a provider the
// user is NOT allowed to access, the request is rejected instead of being
// proxied through the forbidden provider.
func TestPathRoutedSourceProviderACLEnforced(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"secret"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	providerSvc, modelSvc, usageSvc, authSvc := seedACLDB(t, upstream.URL)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, authSvc, nil)

	r := gin.New()
	r.POST("/:provider_key/v1/*endpoint", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(2))
		engine.HandlePathRouted(c)
	})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/my-custom/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", w.Code, w.Body.String())
	}
	if upstreamHit {
		t.Fatalf("upstream secret provider was contacted despite ACL denial")
	}
}

// TestPathRoutedSourceProviderACLAllowsAccess is the positive counterpart:
// when the source provider IS allowed, the request proxies through normally.
func TestPathRoutedSourceProviderACLAllowsAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"cmpl-1","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(upstream.Close)

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (2, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'secret-openai', 'Secret OpenAI', ?, ?, 'openai', NULL)`,
		upstream.URL, encryptTestAPIKey(t, "sk-secret"),
	); err != nil {
		t.Fatalf("seed secret provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (2, 'my-custom', 'My Custom', '', '', 'custom', 2)`,
	); err != nil {
		t.Fatalf("seed custom provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, source_provider_key, user_id)
		 VALUES (1, 2, 'gpt-4o', 'my-custom', 0, 'secret-openai', 2)`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	// Both providers allowed for user 2.
	if _, err := db.Exec(`INSERT INTO user_providers (user_id, provider_id) VALUES (2, 1), (2, 2)`); err != nil {
		t.Fatalf("seed ACL: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 2)`); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, testConfig)
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	authSvc := service.NewAuthService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, authSvc, nil)

	r := gin.New()
	r.POST("/:provider_key/v1/*endpoint", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(2))
		engine.HandlePathRouted(c)
	})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/my-custom/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", w.Code, w.Body.String())
	}
	if !upstreamHit {
		t.Fatalf("upstream was never contacted for an allowed provider")
	}
}
