package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/crypto"
	"omnirelay/internal/database"
	"omnirelay/internal/models"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

const testEncryptKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var testConfig = &config.Config{EncryptKey: testEncryptKey}

func encryptTestAPIKey(t *testing.T, plain string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plain, testEncryptKey)
	if err != nil {
		t.Fatalf("encrypt api key: %v", err)
	}
	return enc
}

func TestExecuteChatLogsUsageForAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1",
			"object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"ok"}}],
			"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
		}`)
	}))
	t.Cleanup(upstream.Close)

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'openai', 'OpenAI', ?, ?, 'openai', 1)`,
		upstream.URL, encryptTestAPIKey(t, "sk-test"),
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'gpt-4o', 'openai', 1, 1)`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, testConfig)
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleChatCompletions(c)
	})

	body := `{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	logs, total, err := usageSvc.Query(models.UsageQueryParams{}, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(logs))
	}
	if logs[0].RequestTokens != 1 || logs[0].ResponseTokens != 1 {
		t.Fatalf("tokens = %+v, want 1/1 (locally counted)", logs[0])
	}
	if logs[0].UserID == nil || *logs[0].UserID != 1 {
		t.Fatalf("user_id = %v, want 1", logs[0].UserID)
	}
}

func TestExecuteChatResolvesUserIDFromAPIKeyWhenContextMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"choices":[{"message":{"role":"assistant","content":"ok"}}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
		}`)
	}))
	t.Cleanup(upstream.Close)

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'openai', 'OpenAI', ?, ?, 'openai', NULL)`,
		upstream.URL, encryptTestAPIKey(t, "sk-test"),
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'gpt-4o', 'openai', 1, NULL)`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, testConfig)
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(0))
		engine.HandleChatCompletions(c)
	})

	body := `{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var dbCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM usage_logs").Scan(&dbCount); err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if dbCount != 1 {
		t.Fatalf("db log count = %d, want 1", dbCount)
	}

	logs, total, err := usageSvc.Query(models.UsageQueryParams{}, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("user 1 should see resolved logs: total=%d len=%d", total, len(logs))
	}
	if logs[0].UserID == nil || *logs[0].UserID != 1 {
		t.Fatalf("resolved user_id = %v, want 1", logs[0].UserID)
	}
}

func TestPathRoutedStreamingWithoutModelStillLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'openai', 'OpenAI', ?, ?, 'openai', 1)`,
		upstream.URL, encryptTestAPIKey(t, "sk-test"),
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, testConfig)
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

	r := gin.New()
	r.POST("/:provider_key/v1/*endpoint", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandlePathRouted(c)
	})

	body := `{"model":"unknown-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	logs, total, err := usageSvc.Query(models.UsageQueryParams{}, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1 after streaming without model", total, len(logs))
	}
	if logs[0].RequestTokens != 1 || logs[0].ResponseTokens != 2 {
		t.Fatalf("tokens = %+v, want 1/2 (local input, upstream output)", logs[0])
	}
}

func TestEmptyUpstreamResponseIsError(t *testing.T) {
	for _, stream := range []bool{false, true} {
		stream := stream
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
			}))
			t.Cleanup(upstream.Close)

			db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatalf("init db: %v", err)
			}
			t.Cleanup(func() { db.Close() })

			if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
				t.Fatalf("seed user: %v", err)
			}
			if _, err := db.Exec(
				`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
				 VALUES (1, 'openai', 'OpenAI', ?, ?, 'openai', 1)`,
				upstream.URL, encryptTestAPIKey(t, "sk-test"),
			); err != nil {
				t.Fatalf("seed provider: %v", err)
			}
			if _, err := db.Exec(
				`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
				 VALUES (1, 1, 'gpt-4o', 'openai', 1, 1)`,
			); err != nil {
				t.Fatalf("seed model: %v", err)
			}
			if _, err := db.Exec(
				`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
			); err != nil {
				t.Fatalf("seed api key: %v", err)
			}

			providerSvc := service.NewProviderService(db, testConfig)
			modelSvc := service.NewModelService(db)
			usageSvc := service.NewUsageService(db)
			engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

			r := gin.New()
			r.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Set("api_key_id", int64(1))
				c.Set("user_id", int64(1))
				engine.HandleChatCompletions(c)
			})

			body := fmt.Sprintf(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":%v}`, stream)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, body = %s, want 502", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "empty response") {
				t.Fatalf("body = %s, want empty-response error", w.Body.String())
			}

			logs, total, err := usageSvc.Query(models.UsageQueryParams{}, 1)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if total != 1 || len(logs) != 1 || !logs[0].IsError {
				t.Fatalf("want one error log, got total=%d len=%d IsError=%v", total, len(logs), logs[0].IsError)
			}
		})
	}
}