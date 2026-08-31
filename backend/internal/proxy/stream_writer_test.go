package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStreamWriterKeepAliveSkippedMidLine(t *testing.T) {
	var buf bytes.Buffer
	sw := newStreamWriter(&buf, &nopFlusher{})

	// Upstream chunk split mid-JSON: no trailing newline.
	sw.Write([]byte(`data: {"type":"response.content_part.done","item_id":"msg_1","output_index":1,`))
	sw.WriteKeepAlive()
	sw.Write([]byte(`"part":{}}` + "\n\n"))
	sw.WriteKeepAlive()

	got := buf.String()
	if bytes.Count([]byte(got), []byte(": keepalive")) != 1 {
		t.Fatalf("keepalive should only appear once (at line boundary); got %q", got)
	}
	if !bytes.HasSuffix([]byte(got), []byte(": keepalive\n\n")) {
		t.Fatalf("keepalive should be the last SSE comment; got %q", got)
	}
	for _, line := range bytes.Split([]byte(got), []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data: ")) {
			payload := bytes.TrimPrefix(line, []byte("data: "))
			if !bytes.Contains(payload, []byte(": keepalive")) {
				continue
			}
			t.Fatalf("keepalive spliced into data payload: %q", got)
		}
	}
}

func TestStreamWriterKeepAliveAtBoundary(t *testing.T) {
	var buf bytes.Buffer
	sw := newStreamWriter(&buf, &nopFlusher{})

	sw.Write([]byte("data: {\"a\":1}\n\n"))
	sw.WriteKeepAlive()

	got := buf.String()
	if got != "data: {\"a\":1}\n\n: keepalive\n\n" {
		t.Fatalf("got %q", got)
	}
}

type nopFlusher struct{}

func (n *nopFlusher) Flush() {}

// A conduit-style Responses SSE stream whose message text is a leaked
// functions.exec tool call must be reassembled across chunk boundaries and
// converted into function_call items by buildResponsesToolCallEvents.
func TestResponsesStreamLeakedToolCallRewrite(t *testing.T) {
	stream := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_abc123\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"<tool_call>\\n<function=functions.exec>\\n\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"<parameter=tools.exec_command>{\\\"cmd\\\": \\\"ls -la\\\"}</parameter>\\n</function>\\n</tool_call>\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_abc123\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15}}}\n\n"

	var rb responsesBuffer
	// Feed in awkwardly-sized pieces to exercise line reassembly.
	raw := []byte(stream)
	for len(raw) > 0 {
		n := 64
		if n > len(raw) {
			n = len(raw)
		}
		if !rb.observe(raw[:n]) {
			t.Fatal("chunk not recognized as Responses stream")
		}
		raw = raw[n:]
	}

	text := rb.finalText
	if text == "" {
		text = rb.deltaAccum.String()
	}
	tcs, ok := parseContentAsToolCalls(text, nil)
	if !ok || len(tcs) != 1 {
		t.Fatalf("leak not recovered: ok=%v tcs=%v", ok, tcs)
	}
	fn, _ := tcs[0]["function"].(map[string]interface{})
	if name, _ := fn["name"].(string); name != "functions.exec" {
		t.Errorf("name = %q, want functions.exec", name)
	}
	if args, _ := fn["arguments"].(string); args != `{"tools.exec_command":{"cmd":"ls -la"}}` {
		t.Errorf("args = %q", args)
	}

	out := string(buildResponsesToolCallEvents(&rb, tcs, "conduit/gpt-5.6-sol"))
	for _, want := range []string{
		"\"type\":\"response.created\"",
		"\"id\":\"resp_abc123\"",
		"\"type\":\"function_call\"",
		"\"name\":\"functions.exec\"",
		"\"call_id\":\"call_",
		"\"type\":\"response.function_call_arguments.done\"",
		"\"type\":\"response.output_item.done\"",
		"\"type\":\"response.completed\"",
		"\"input_tokens\":10",
	} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("synthetic stream missing %s:\n%s", want, out)
		}
	}
	if bytes.Contains([]byte(out), []byte("<tool_call>")) {
		t.Error("synthetic stream still contains the leaked text")
	}
}

// A Responses stream that already carries a real function_call item must not
// be treated as a leak candidate.
func TestResponsesStreamWithRealToolCallNotLeak(t *testing.T) {
	stream := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"functions.exec\",\"arguments\":\"{}\"}}\n\n"
	var rb responsesBuffer
	rb.observe([]byte(stream))
	if !bytes.Contains(rb.buf.Bytes(), []byte(`"function_call"`)) {
		t.Fatal("function_call marker not detected in buffer")
	}
}

// End-to-end: a path-routed /v1/responses request whose upstream Responses
// stream leaks the tool call as message text must reach the client as
// function_call items (the Codex wire_api="responses" setup).
func TestPathRoutedResponsesLeakRewrittenEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_e2e\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
		io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n")
		io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"delta\":\"<tool_call>\\n\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"delta\":\"<function=functions.exec>\\n<parameter=tools.exec_command>{\\\"cmd\\\": \\\"ls -la\\\"}</parameter>\\n</function>\\n</tool_call>\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"text\":\"<tool_call>\\n<function=functions.exec>\\n<parameter=tools.exec_command>{\\\"cmd\\\": \\\"ls -la\\\"}</parameter>\\n</function>\\n</tool_call>\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"leaked\"}]}}\n\n")
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_e2e\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":20900,\"output_tokens\":57,\"total_tokens\":20957}}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)

	router := newInterruptionTestRouter(t, upstream, "openai")

	body := `{"model":"p/m","stream":true,"tools":[{"type":"function","name":"functions.exec","parameters":{"type":"object"}}],"input":"list files"}`
	req := httptest.NewRequest(http.MethodPost, "/p/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	respBody := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, respBody)
	}
	for _, want := range []string{
		"\"type\":\"response.created\"",
		"\"id\":\"resp_e2e\"",
		"\"type\":\"function_call\"",
		"\"name\":\"functions.exec\"",
		`\"tools.exec_command\":{\"cmd\":\"ls -la\"}`,
		"\"type\":\"response.completed\"",
		"data: [DONE]",
	} {
		if !strings.Contains(respBody, want) {
			t.Errorf("response missing %s:\n%s", want, respBody)
		}
	}
	if strings.Contains(respBody, "<tool_call>") || strings.Contains(respBody, "<function=functions.exec>") {
		t.Errorf("leaked tool call text reached the client:\n%s", respBody)
	}
}
