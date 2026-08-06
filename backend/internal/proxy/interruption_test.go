package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestHandleStreamResponseInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + interruptionMsg + "\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, 1, "anthropic", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "data: ") {
		t.Errorf("stream data leaked into error body: %s", w.Body.String())
	}
}

func TestHandleMessagesStreamResponseInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + interruptionMsg + "\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleMessagesStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, time.Now(), 1, "anthropic", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"overloaded_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHandleStreamResponseEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Empty message\"}}]}\n\n" +
		"data: [DONE]\n\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleStreamResponse(c, upstream, &OpenAIAdapter{}, 1, 1, "openai/gpt-4o", nil, 1, "openai", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "data: ") {
		t.Errorf("stream data leaked into error body: %s", w.Body.String())
	}
}

func TestHandleMessagesStreamResponseEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Empty message\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleMessagesStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, time.Now(), 1, "anthropic", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"overloaded_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "data: ") {
		t.Errorf("stream data leaked into error body: %s", w.Body.String())
	}
}

func TestHandleMessagesStreamResponseRequestFailedStill502(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Request failed.\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleMessagesStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, time.Now(), 1, "anthropic", 5)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"api_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHandleStreamResponseNormalStreamStill200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi there\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, 1, "anthropic", 5)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"content":"hi there"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Errorf("missing [DONE]: %s", w.Body.String())
	}
}
