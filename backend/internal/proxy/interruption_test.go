package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

const interruptionMsg = "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."

func newInterruptionTestRouter(t *testing.T, upstream *httptest.Server, providerType string) *gin.Engine {
	t.Helper()
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
		 VALUES (1, 'p', 'P', ?, ?, ?, 1)`,
		upstream.URL, encryptTestAPIKey(t, "sk-test"), providerType,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'm', 'p', 1, 1)`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, &config.Config{EncryptKey: testEncryptKey})
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleChatCompletions(c)
	})
	r.POST("/v1/messages", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleMessages(c)
	})
	r.Any("/:provider_key/v1/*endpoint", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandlePathRouted(c)
	})
	return r
}

func TestNonStreamChatInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1","object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"`+interruptionMsg+`"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "openai")

	body := `{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestNonStreamMessagesInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant",
			"content":[{"type":"text","text":"`+interruptionMsg+`"}],
			"model":"claude-opus-4-8","stop_reason":"end_turn",
			"usage":{"input_tokens":5,"output_tokens":2}
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "anthropic")

	body := `{"model":"p/m","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"overloaded_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestPathRoutedInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1","object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"`+interruptionMsg+`"},"finish_reason":"stop"}]
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "openai")

	body := `{"model":"p/unknown-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/p/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestNonStreamChatEmptyMessageStill200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1","object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"Empty message"},"finish_reason":"stop"}]
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "openai")

	body := `{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("legacy status = %d, body = %s", w.Code, w.Body.String())
	}
	var respObj map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respObj); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if errObj, ok := respObj["error"].(map[string]interface{}); !ok || errObj["message"] != "Empty message" {
		t.Errorf("expected legacy error body, got %s", w.Body.String())
	}
}
