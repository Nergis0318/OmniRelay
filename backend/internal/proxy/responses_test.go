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

func TestResponsesToChatBodyStringInput(t *testing.T) {
	body := map[string]interface{}{
		"model":             "openai/gpt-4o",
		"input":             "hello",
		"instructions":      "be nice",
		"max_output_tokens": float64(100),
		"temperature":       0.5,
		"stream":            true,
	}
	chat, err := responsesToChatBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v", chat["model"])
	}
	if chat["max_tokens"] != float64(100) {
		t.Errorf("max_tokens = %v", chat["max_tokens"])
	}
	if chat["temperature"] != 0.5 || chat["stream"] != true {
		t.Errorf("passthrough fields = %#v", chat)
	}
	messages := chat["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d", len(messages))
	}
	if messages[0].(map[string]interface{})["role"] != "system" || messages[0].(map[string]interface{})["content"] != "be nice" {
		t.Errorf("first message = %#v", messages[0])
	}
	if messages[1].(map[string]interface{})["role"] != "user" || messages[1].(map[string]interface{})["content"] != "hello" {
		t.Errorf("second message = %#v", messages[1])
	}
}

func TestResponsesToChatBodyInputArray(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "what's the weather?"},
		}},
		map[string]interface{}{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": map[string]interface{}{"city": "seoul"}},
		map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": "sunny"},
		map[string]interface{}{"type": "function_call", "call_id": "call_2", "name": "get_temperature", "arguments": `{"city":"seoul","unit":"c"}`},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": input})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := chat["messages"].([]interface{})
	if len(messages) != 4 {
		t.Fatalf("messages len = %d", len(messages))
	}
	if messages[0].(map[string]interface{})["role"] != "user" {
		t.Errorf("messages[0] role = %v", messages[0].(map[string]interface{})["role"])
	}
	parts := messages[0].(map[string]interface{})["content"].([]map[string]interface{})
	if parts[0]["type"] != "text" || parts[0]["text"] != "what's the weather?" {
		t.Errorf("messages[0] content = %#v", messages[0].(map[string]interface{})["content"])
	}
	if messages[1].(map[string]interface{})["role"] != "assistant" {
		t.Errorf("messages[1] role = %v", messages[1].(map[string]interface{})["role"])
	}
	toolCalls := messages[1].(map[string]interface{})["tool_calls"].([]map[string]interface{})
	fn := toolCalls[0]["function"].(map[string]interface{})
	if fn["name"] != "get_weather" || fn["arguments"] != `{"city":"seoul"}` {
		t.Errorf("tool call = %#v", toolCalls[0])
	}
	if messages[2].(map[string]interface{})["role"] != "tool" || messages[2].(map[string]interface{})["tool_call_id"] != "call_1" || messages[2].(map[string]interface{})["content"] != "sunny" {
		t.Errorf("messages[2] = %#v", messages[2])
	}
	stringFn := messages[3].(map[string]interface{})["tool_calls"].([]map[string]interface{})[0]["function"].(map[string]interface{})
	if stringFn["arguments"] != `{"city":"seoul","unit":"c"}` {
		t.Errorf("string arguments = %#v", stringFn["arguments"])
	}
}

func TestResponsesToChatBodyInputImage(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "input_image", "image_url": "https://example.com/cat.png", "detail": "high"},
		}},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": input})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := chat["messages"].([]interface{})[0].(map[string]interface{})["content"].([]map[string]interface{})
	img := parts[0]["image_url"].(map[string]interface{})
	if parts[0]["type"] != "image_url" || img["url"] != "https://example.com/cat.png" || img["detail"] != "high" {
		t.Errorf("image part = %#v", parts[0])
	}
}

func TestResponsesToChatBodyTools(t *testing.T) {
	tools := []interface{}{
		map[string]interface{}{"type": "function", "name": "get_weather", "description": "d", "strict": true, "parameters": map[string]interface{}{"type": "object"}},
		map[string]interface{}{"type": "web_search"},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": "hi", "tools": tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	converted := chat["tools"].([]map[string]interface{})
	if len(converted) != 1 {
		t.Fatalf("tools len = %d (web_search should be dropped)", len(converted))
	}
	fn := converted[0]["function"].(map[string]interface{})
	if converted[0]["type"] != "function" || fn["name"] != "get_weather" || fn["strict"] != true {
		t.Errorf("converted tool = %#v", converted[0])
	}
}

func TestResponsesToChatBodyInvalidInput(t *testing.T) {
	if _, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": 42}); err == nil {
		t.Errorf("expected error for numeric input")
	}
}

func TestChatResponseToResponses(t *testing.T) {
	chat := map[string]interface{}{
		"id": "cmpl-1",
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "hello",
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":       "call_9",
							"type":     "function",
							"function": map[string]interface{}{"name": "get_weather", "arguments": `{"city":"seoul"}`},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]interface{}{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7},
	}
	resp := chatResponseToResponses(chat, "openai/gpt-4o")

	if resp["object"] != "response" || resp["model"] != "openai/gpt-4o" || resp["status"] != "completed" {
		t.Errorf("base fields = %#v", resp)
	}
	if resp["output_text"] != "hello" {
		t.Errorf("output_text = %v", resp["output_text"])
	}
	output := resp["output"].([]map[string]interface{})
	if len(output) != 2 {
		t.Fatalf("output len = %d", len(output))
	}
	if output[0]["type"] != "message" || output[1]["type"] != "function_call" {
		t.Errorf("output types = %v, %v", output[0]["type"], output[1]["type"])
	}
	fc := output[1]
	if fc["call_id"] != "call_9" || fc["name"] != "get_weather" || fc["arguments"] != `{"city":"seoul"}` {
		t.Errorf("function_call = %#v", fc)
	}
	usage := resp["usage"].(map[string]interface{})
	if usage["input_tokens"] != int64(4) || usage["output_tokens"] != int64(3) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestChatResponseToResponsesIncomplete(t *testing.T) {
	chat := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"message":       map[string]interface{}{"role": "assistant", "content": "part"},
			"finish_reason": "length",
		}},
	}
	resp := chatResponseToResponses(chat, "openai/gpt-4o")
	if resp["status"] != "incomplete" {
		t.Errorf("status = %v", resp["status"])
	}
}

func TestHandleResponsesStreamText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n" +
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
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleResponsesStream(c, upstream, &OpenAIAdapter{}, 1, 1, "openai/gpt-4o", nil, 1, "openai", 5)

	body := w.Body.String()
	for _, want := range []string{
		`"type":"response.created"`,
		`"type":"response.output_text.delta"`,
		`"delta":"Hel"`,
		`"delta":"lo"`,
		`"type":"response.output_text.done"`,
		`"type":"response.output_item.done"`,
		`"type":"response.completed"`,
		`"status":"completed"`,
		`"output_text":"Hello"`,
		`"input_tokens":5`,
		`"output_tokens":3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %s\nbody: %s", want, body)
		}
	}
}

func TestHandleResponsesStreamToolCall(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"seoul\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
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
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleResponsesStream(c, upstream, &OpenAIAdapter{}, 1, 1, "openai/gpt-4o", nil, 1, "openai", 0)

	body := w.Body.String()
	for _, want := range []string{
		`"type":"function_call"`,
		`"type":"response.function_call_arguments.delta"`,
		`"type":"response.function_call_arguments.done"`,
		`"arguments":"{\"city\":\"seoul\"}"`,
		`"call_id":"call_1"`,
		`"name":"get_weather"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %s\nbody: %s", want, body)
		}
	}
}

func newResponsesTestRouter(t *testing.T, upstream *httptest.Server, providerType string) (*gin.Engine, *Engine) {
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
		 VALUES (1, 'openai', 'OpenAI', ?, ?, ?, 1)`,
		upstream.URL, encryptTestAPIKey(t, "sk-test"), providerType,
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

	providerSvc := service.NewProviderService(db, &config.Config{EncryptKey: testEncryptKey})
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

	r := gin.New()
	r.POST("/v1/responses", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleResponses(c)
	})
	return r, engine
}

func TestHandleResponsesNonStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1",
			"object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
		}`)
	}))
	t.Cleanup(upstream.Close)

	r, _ := newResponsesTestRouter(t, upstream, "openai")

	body := `{"model":"openai/gpt-4o","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var respObj map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respObj); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if respObj["object"] != "response" {
		t.Errorf("object = %v", respObj["object"])
	}
	if respObj["output_text"] != "hi there" {
		t.Errorf("output_text = %v", respObj["output_text"])
	}
	if respObj["status"] != "completed" {
		t.Errorf("status = %v", respObj["status"])
	}
	usage := respObj["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(4) || usage["output_tokens"] != float64(2) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestHandleResponsesStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w,
			"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"+
				"data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)

	r, _ := newResponsesTestRouter(t, upstream, "openai")

	body := `{"model":"openai/gpt-4o","input":"hello","stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"response.output_text.delta"`) {
		t.Errorf("stream missing output_text.delta\nbody: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"response.completed"`) {
		t.Errorf("stream missing response.completed\nbody: %s", w.Body.String())
	}
	if !strings.HasSuffix(w.Body.String(), "data: [DONE]\n\n") {
		t.Errorf("stream missing [DONE] terminator\nbody: %s", w.Body.String())
	}
}

func TestHandleResponsesStreamAnthropic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w,
			"event: content_block_delta\n"+
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi \"}}\n\n"+
				"event: content_block_delta\n"+
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"there\"}}\n\n"+
				"event: message_delta\n"+
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n"+
				"event: message_stop\n"+
				"data: {\"type\":\"message_stop\"}\n")
	}))
	t.Cleanup(upstream.Close)

	r, _ := newResponsesTestRouter(t, upstream, "anthropic")

	body := `{"model":"openai/gpt-4o","input":"hello","stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	respBody := w.Body.String()
	for _, want := range []string{
		`"type":"response.output_text.delta"`,
		`"delta":"hi "`,
		`"delta":"there"`,
		`"type":"response.completed"`,
		`"output_text":"hi there"`,
		`"output_tokens":5`,
		"data: [DONE]",
	} {
		if !strings.Contains(respBody, want) {
			t.Errorf("stream missing %s\nbody: %s", want, respBody)
		}
	}
}

func TestHandleResponsesStreamGemini(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w,
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n"+
				"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]}}]}\n\n"+
				"data: {\"usageMetadata\":{\"promptTokenCount\":4,\"candidatesTokenCount\":2,\"totalTokenCount\":6}}\n\n")
	}))
	t.Cleanup(upstream.Close)

	r, _ := newResponsesTestRouter(t, upstream, "gemini")

	body := `{"model":"openai/gpt-4o","input":"hello","stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	respBody := w.Body.String()
	for _, want := range []string{
		`"type":"response.output_text.delta"`,
		`"delta":"hel"`,
		`"delta":"lo"`,
		`"type":"response.completed"`,
		`"output_text":"hello"`,
		`"output_tokens":2`,
		"data: [DONE]",
	} {
		if !strings.Contains(respBody, want) {
			t.Errorf("stream missing %s\nbody: %s", want, respBody)
		}
	}
}

func TestHandleResponsesStreamSplitEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	bigArgs := strings.Repeat("a", 6000)
	sse := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"" + bigArgs + "\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
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
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleResponsesStream(c, upstream, &OpenAIAdapter{}, 1, 1, "openai/gpt-4o", nil, 1, "openai", 0)

	body := w.Body.String()
	if !strings.Contains(body, `"arguments":"`+bigArgs+`"`) {
		t.Errorf("split event arguments truncated (want %d chars)\nbody: %s", len(bigArgs), body)
	}
}

func TestHandleResponsesStreamInterruption503(t *testing.T) {
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
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleResponsesStream(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, 1, "anthropic", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "response.created") {
		t.Errorf("response events leaked into error body: %s", w.Body.String())
	}
}
