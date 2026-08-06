package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

func newResponsesEmptyEngine(t *testing.T) (*Engine, *service.UsageService) {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	return NewEngine(nil, nil, usageSvc, nil, nil), usageSvc
}

func responsesEmptySSE(parts string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(parts)),
	}
}

func TestHandleStreamResponseResponsesEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"[Empty\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" message]\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"[Empty message]\",\"annotations\":[]}]}],\"output_text\":\"[Empty message]\"}}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, responsesEmptySSE(sse), &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

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

func TestHandleStreamResponseResponsesDoneOnlyEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Some upstreams skip delta events and only send the final text.
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, responsesEmptySSE(sse), &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHandleStreamResponseResponsesNormalStream200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hi there\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"hi there\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hi there\",\"annotations\":[]}]}],\"output_text\":\"hi there\"}}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, responsesEmptySSE(sse), &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"delta":"hi there"`) {
		t.Errorf("buffered stream not replayed: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Errorf("missing [DONE]: %s", w.Body.String())
	}
}

func TestHandleRawStreamResponseResponsesEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"[Empty\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" message]\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[],\"output_text\":\"[Empty message]\"}}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleRawStreamResponse(c, responsesEmptySSE(sse), 1, 1, "conduit/gpt-5.6-sol", time.Now(), 1)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}
