package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

func seedKeyFailoverRouter(t *testing.T, upstream *httptest.Server, keys []string) (*gin.Engine, *service.ProviderService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'p', 'P', ?, '', 'openai', 1)`,
		upstream.URL,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'm', 'p', 1, 1)`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{EncryptKey: testEncryptKey}
	ps := service.NewProviderService(db, cfg)
	for _, k := range keys {
		if _, err := ps.AddKey(1, 1, k); err != nil {
			t.Fatal(err)
		}
	}
	engine := NewEngine(ps, service.NewModelService(db), service.NewUsageService(db), nil, nil)
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleChatCompletions(c)
	})
	return r, ps
}

func chatBody() string {
	return `{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`
}

func TestKeyRoundRobin(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	r, _ := seedKeyFailoverRouter(t, upstream, []string{"key-a", "key-b"})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("call %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if len(seen) < 2 || seen[0] == seen[1] {
		t.Fatalf("RR keys = %v", seen)
	}
}

func TestFailoverOn429(t *testing.T) {
	var n atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(429)
			io.WriteString(w, `{"error":"rate"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	r, _ := seedKeyFailoverRouter(t, upstream, []string{"key-a", "key-b"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if n.Load() < 2 {
		t.Fatalf("did not retry, hits=%d", n.Load())
	}
}

func Test401DeactivatesKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "bad-key" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"nope"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	r, ps := seedKeyFailoverRouter(t, upstream, []string{"bad-key", "good-key"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	p, _ := ps.GetByID(1, 1)
	var activeBad bool
	for _, k := range p.APIKeys {
		if k.KeyPrefix == "bad-key" && k.IsActive {
			activeBad = true
		}
	}
	if activeBad {
		t.Fatal("401 key still active")
	}
}

func TestAllKeysFailReturnsOneError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		io.WriteString(w, `{"error":"down"}`)
	}))
	defer upstream.Close()
	r, _ := seedKeyFailoverRouter(t, upstream, []string{"k1", "k2"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("expected error")
	}
	var payload map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &payload)
	if payload == nil {
		t.Fatalf("empty body %s", w.Body.String())
	}
}
