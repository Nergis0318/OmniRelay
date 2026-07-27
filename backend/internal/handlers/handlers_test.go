package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func setupTest(t *testing.T) (*sql.DB, *service.ProviderService, *service.ModelService, *service.APIKeyService, *service.UsageService, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create a test user
	db.Exec(`INSERT INTO users (id, username, password_hash, is_admin) VALUES (1, 'admin', 'hash', 1)`)

	// Generate a JWT for the test user
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  float64(1),
		"username": "admin",
	})
	tokenStr, _ := token.SignedString([]byte("test-secret"))

	testCfg := &config.Config{
		EncryptKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		JWTSecret:  "test-secret",
	}

	providerSvc := service.NewProviderService(db, testCfg)
	modelSvc := service.NewModelService(db)
	apiKeySvc := service.NewAPIKeyService(db)
	usageSvc := service.NewUsageService(db)

	return db, providerSvc, modelSvc, apiKeySvc, usageSvc, tokenStr
}

func adminAuthMiddleware(tokenStr string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", int64(1))
		c.Set("username", "admin")
		c.Set("is_admin", true)
		c.Next()
	}
}

func TestListProviders(t *testing.T) {
	db, ps, _, _, _, tokenStr := setupTest(t)
	defer db.Close()

	authSvc := service.NewAuthService(db)

	r := gin.New()
	r.GET("/admin/providers", adminAuthMiddleware(tokenStr), ListProviders(ps, authSvc))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/providers", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Should return empty list, not null
	if !strings.Contains(w.Body.String(), `"providers":[]`) && !strings.Contains(w.Body.String(), `"providers":\[`) {
		t.Logf("body: %s", w.Body.String())
	}
}

func TestListModels(t *testing.T) {
	db, _, ms, _, _, tokenStr := setupTest(t)
	defer db.Close()

	authSvc := service.NewAuthService(db)

	r := gin.New()
	r.GET("/admin/models", adminAuthMiddleware(tokenStr), ListModels(ms, authSvc))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/models", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListAPIKeys(t *testing.T) {
	db, _, _, aks, _, tokenStr := setupTest(t)
	defer db.Close()

	r := gin.New()
	r.GET("/admin/api-keys", adminAuthMiddleware(tokenStr), ListAPIKeys(aks))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api-keys", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateModel(t *testing.T) {
	db, _, ms, _, _, tokenStr := setupTest(t)
	defer db.Close()

	// Need a provider first
	db.Exec(`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id) VALUES (1, 'test', 'Test', 'http://localhost', 'enc', 'openai', 1)`)

	r := gin.New()
	r.POST("/admin/models", adminAuthMiddleware(tokenStr), CreateModel(ms))

	body := `{"model_id":"gpt-4o","display_name":"GPT-4o","provider_id":1,"input_price_per_1mtok":2.5,"output_price_per_1mtok":10}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateModelInvalidJSON(t *testing.T) {
	db, _, ms, _, _, tokenStr := setupTest(t)
	defer db.Close()

	r := gin.New()
	r.POST("/admin/models", adminAuthMiddleware(tokenStr), CreateModel(ms))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/models", strings.NewReader(`invalid json`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListUsage(t *testing.T) {
	db, _, _, _, us, tokenStr := setupTest(t)
	defer db.Close()

	// Seed some usage data
	db.Exec(`INSERT INTO usage_logs (model, request_tokens, response_tokens, total_tokens, user_id) VALUES ('gpt-4', 10, 20, 30, 1)`)

	r := gin.New()
	r.GET("/admin/usage", adminAuthMiddleware(tokenStr), ListUsage(us))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/usage", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetStats(t *testing.T) {
	db, _, ms, aks, us, tokenStr := setupTest(t)
	defer db.Close()

	db.Exec(`INSERT INTO usage_logs (model, request_tokens, response_tokens, total_tokens, user_id) VALUES ('gpt-4', 10, 20, 30, 1)`)

	r := gin.New()
	r.GET("/admin/stats", adminAuthMiddleware(tokenStr), GetStats(us, aks, ms))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteModel(t *testing.T) {
	db, _, ms, _, _, tokenStr := setupTest(t)
	defer db.Close()

	db.Exec(`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id) VALUES (1, 'test', 'Test', 'http://localhost', 'enc', 'openai', 1)`)
	db.Exec(`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id) VALUES (1, 1, 'gpt-4', 'test', 1, 1)`)

	r := gin.New()
	r.DELETE("/admin/models/:id", adminAuthMiddleware(tokenStr), DeleteModel(ms))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/models/1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Delete non-existing (idempotent)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/admin/models/999", nil)
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for idempotent delete, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestRegisterLoginFlow(t *testing.T) {
	db, _, _, _, _, _ := setupTest(t)
	defer db.Close()

	authSvc := service.NewAuthService(db)
	authSvc.SetJWTSecret("test-secret")

	r := gin.New()
	r.POST("/admin/auth/register", Register(authSvc))
	r.POST("/admin/auth/login", Login(authSvc))

	// Register
	registerBody := `{"username":"newuser","email":"new@test.com","password":"pass123"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/register", strings.NewReader(registerBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("register expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Login
	loginBody := `{"email":"new@test.com","password":"pass123"}`
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/admin/auth/login", strings.NewReader(loginBody))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("login expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// Duplicate register
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/admin/auth/register", strings.NewReader(registerBody))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusConflict {
		t.Errorf("duplicate register expected 409, got %d", w3.Code)
	}
}
