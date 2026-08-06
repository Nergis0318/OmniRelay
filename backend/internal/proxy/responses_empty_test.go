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

// chunkedReader returns one preset string per Read call, simulating upstream
// reads that split SSE events at arbitrary boundaries.
type chunkedReader struct {
	chunks []string
	i      int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.i >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.i])
	r.i++
	return n, nil
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

// TestHandleStreamResponseResponsesErrorAfterFlush verifies that when the 200
// header is already committed (first chunk flushed live, or keepalive) before
// the "[Empty message]" decision, the error is emitted as an SSE-framed event
// instead of an ignored bare WriteHeader(503).
func TestHandleStreamResponseResponsesErrorAfterFlush(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// First chunk flushes the head live (data: [DONE] sets sentDone), committing
	// HTTP 200. The trigger arrives only in later chunks, which buffer, then
	// "[Empty message]" at stream end must surface as an SSE error event.
	sse := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(&chunkedReader{chunks: []string{
			"data: [DONE]\n\n",
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"[Empty\"}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" message]\"}\n\n" +
				"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"[Empty message]\",\"annotations\":[]}]}],\"output_text\":\"[Empty message]\"}}\n\n" +
				"data: [DONE]\n\n",
		}}),
	}

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, sse, &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, body)
	}
	if !strings.Contains(body, `data: {"error"`) {
		t.Errorf("SSE error event missing: %s", body)
	}
	if !strings.Contains(body, `"message":"[Empty message]"`) {
		t.Errorf("missing error message: %s", body)
	}
	if !strings.Contains(body, `"type":"server_error"`) {
		t.Errorf("missing server_error type: %s", body)
	}
	if strings.Contains(body, "\n{\"error\"") {
		t.Errorf("stray unframed error JSON: %s", body)
	}
}

// TestHandleStreamResponseResponsesSplitTrigger verifies that a trigger
// substring split across two reads (chunk ends with `"type":"respon`, next
// starts with `se.`) still activates responsesMode and yields the clean 503.
func TestHandleStreamResponseResponsesSplitTrigger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(&chunkedReader{chunks: []string{
			": hello\n\ndata: {\"type\":\"respon",
			"se.{\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"[Empty\"}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" message]\"}\n\n" +
				"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"[Empty message]\",\"annotations\":[]}]}],\"output_text\":\"[Empty message]\"}}\n\n" +
				"data: [DONE]\n\n",
		}}),
	}

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, sse, &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, body)
	}
	if !strings.Contains(body, `"type":"server_error"`) {
		t.Errorf("body = %s", body)
	}
	if strings.Contains(body, "data: ") {
		t.Errorf("stream data leaked into error body: %s", body)
	}
}
