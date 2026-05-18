package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCopyForwardableRequestHeadersCopiesClientHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Accept-Language", "ko-KR")
	c.Request.Header.Set("OpenAI-Beta", "assistants=v2")
	c.Request.Header.Set("User-Agent", "test-client/1.0")
	c.Request.Header.Set("X-Custom-Header", "custom-value")

	req := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	copyForwardableRequestHeaders(c, req)

	for name, want := range map[string]string{
		"Accept-Language": "ko-KR",
		"OpenAI-Beta":     "assistants=v2",
		"User-Agent":      "test-client/1.0",
		"X-Custom-Header": "custom-value",
	} {
		if got := req.Header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestCopyForwardableRequestHeadersSkipsUnsafeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Authorization", "Bearer local-key")
	c.Request.Header.Set("Connection", "X-Connection-Scoped")
	c.Request.Header.Set("Keep-Alive", "timeout=5")
	c.Request.Header.Set("X-Api-Key", "local-api-key")
	c.Request.Header.Set("X-Connection-Scoped", "scoped-value")
	c.Request.Header.Set("X-Goog-Api-Key", "local-google-key")

	req := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1/chat/completions", nil)
	copyForwardableRequestHeaders(c, req)

	for _, name := range []string{"Authorization", "Connection", "Keep-Alive", "X-Api-Key", "X-Connection-Scoped", "X-Goog-Api-Key"} {
		if got := req.Header.Get(name); got != "" {
			t.Fatalf("%s should not be forwarded, got %q", name, got)
		}
	}
}

func TestOpenAIParseMessagesStreamChunkConvertsToAnthropicEvents(t *testing.T) {
	adapter := &OpenAIAdapter{}
	state := make(map[string]interface{})

	chunk := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n"

	got, inputTokens, outputTokens, err := adapter.ParseMessagesStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("ParseMessagesStreamChunk returned error: %v", err)
	}

	text := string(got)
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"\"text\":\"hello\"",
		"event: content_block_stop",
		"event: message_delta",
		"\"stop_reason\":\"end_turn\"",
		"event: message_stop",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("converted stream missing %q in:\n%s", want, text)
		}
	}

	if inputTokens != 3 || outputTokens != 2 {
		t.Fatalf("tokens = (%d, %d), want (3, 2)", inputTokens, outputTokens)
	}
}

func TestOpenAIParseMessagesStreamChunkKeepsStateAcrossChunks(t *testing.T) {
	adapter := &OpenAIAdapter{}
	state := make(map[string]interface{})

	first, _, _, err := adapter.ParseMessagesStreamChunk([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"), state)
	if err != nil {
		t.Fatalf("first chunk returned error: %v", err)
	}
	second, _, _, err := adapter.ParseMessagesStreamChunk([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n"), state)
	if err != nil {
		t.Fatalf("second chunk returned error: %v", err)
	}

	if strings.Count(string(first)+string(second), "event: message_start") != 1 {
		t.Fatalf("message_start should only be emitted once; first=%q second=%q", first, second)
	}
}
