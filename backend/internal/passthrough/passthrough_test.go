package passthrough

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"omnirelay/internal/models"
)

type collector struct {
	mu   sync.Mutex
	recs []models.PassthroughLog
}

func (c *collector) log(rec models.PassthroughLog) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, rec)
}

func (c *collector) last(t *testing.T) models.PassthroughLog {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recs) == 0 {
		t.Fatal("no passthrough record was emitted")
	}
	return c.recs[len(c.recs)-1]
}

func notFound(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }

func TestIsPassthroughPath(t *testing.T) {
	cases := map[string]bool{
		"/https://api.openai.com/v1/chat/completions": true,
		"/https:/api.openai.com/v1/chat/completions":  true,
		"/http://127.0.0.1:11434/api/chat":            true,
		"/openai/v1/chat/completions":                 false,
		"/v1/chat/completions":                        false,
		"/https://":                                   false,
		"/ftp://host/x":                               false,
		"/health":                                     false,
	}
	for path, want := range cases {
		if got := IsPassthroughPath(path); got != want {
			t.Errorf("IsPassthroughPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		path       string
		wantURL    string
		wantErrSub string
	}{
		{path: "/https://api.openai.com/v1/chat/completions", wantURL: "https://api.openai.com/v1/chat/completions"},
		{path: "/https:/api.openai.com/v1/chat/completions", wantURL: "https://api.openai.com/v1/chat/completions"},
		{path: "/http://127.0.0.1:11434/api/chat", wantURL: "http://127.0.0.1:11434/api/chat"},
		{path: "/https://host.example/", wantURL: "https://host.example/"},
		{path: "/openai/v1/chat/completions", wantErrSub: "must start with http:// or https://"},
		{path: "/https://", wantErrSub: "missing upstream host"},
		{path: "/https://user:secret@host.example/v1/x", wantErrSub: "credentials"},
	}
	for _, tc := range cases {
		target, err := ParseTarget(tc.path)
		if tc.wantErrSub != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Errorf("ParseTarget(%q) error = %v, want one containing %q", tc.path, err, tc.wantErrSub)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTarget(%q) unexpected error: %v", tc.path, err)
			continue
		}
		if got := target.String(); got != tc.wantURL {
			t.Errorf("ParseTarget(%q) = %q, want %q", tc.path, got, tc.wantURL)
		}
	}
}

func TestBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.10", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fc00::1", "224.0.0.1"}
	for _, raw := range blocked {
		if !BlockedIP(net.ParseIP(raw)) {
			t.Errorf("BlockedIP(%s) = false, want true", raw)
		}
	}
	allowed := []string{"8.8.8.8", "142.250.0.1", "2606:4700:4700::1111"}
	for _, raw := range allowed {
		if BlockedIP(net.ParseIP(raw)) {
			t.Errorf("BlockedIP(%s) = true, want false", raw)
		}
	}
}

// echoUpstream reports what the relay actually sent it.
func echoUpstream(t *testing.T, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAll(r)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Marker", "seen")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"method":%q,"path":%q,"query":%q,"authorization":%q,"x_api_key":%q,"custom":%q,"body":%q}`,
			r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("x-api-key"), r.Header.Get("X-Custom"), body)
	}))
}

func readAll(r *http.Request) (string, error) {
	if r.Body == nil {
		return "", nil
	}
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	return string(data), err
}

func TestRelayForwardsRequestVerbatim(t *testing.T) {
	upstream := echoUpstream(t, http.StatusOK)
	defer upstream.Close()

	var sink collector
	relay := New(Options{AllowPrivate: true, Timeout: 5 * time.Second}, sink.log, http.HandlerFunc(notFound))

	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-upstream-secret")
	req.Header.Set("x-api-key", "sk-anthropic-secret")
	req.Header.Set("X-Custom", "keep-me")
	req.Header.Set("Connection", "keep-alive, X-Drop-Me")
	req.Header.Set("X-Drop-Me", "gone")

	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	for _, want := range []string{
		`"method":"POST"`,
		`"path":"/v1/chat/completions"`,
		`"authorization":"Bearer sk-upstream-secret"`,
		`"x_api_key":"sk-anthropic-secret"`,
		`"custom":"keep-me"`,
		`"body":"{\"model\":\"gpt-4o\"}"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("upstream did not receive %s; echoed %s", want, got)
		}
	}
	if strings.Contains(got, `"gone"`) {
		t.Errorf("header named by Connection was forwarded: %s", got)
	}
	if rec.Header().Get("X-Upstream-Marker") != "seen" {
		t.Error("upstream response header was not passed through")
	}
	if rec.Header().Get(HeaderRelay) != "passthrough" {
		t.Error("missing X-Omni-Relay header")
	}

	record := sink.last(t)
	if record.Host != strings.TrimPrefix(upstream.URL, "http://") {
		t.Errorf("host = %q, want %q", record.Host, upstream.URL)
	}
	if record.Path != "/v1/chat/completions" {
		t.Errorf("path = %q", record.Path)
	}
	if record.StatusCode != http.StatusOK || record.IsError {
		t.Errorf("record = %+v, want 200/non-error", record)
	}
	if record.Method != http.MethodPost {
		t.Errorf("method = %q", record.Method)
	}
	if record.TTFBMs == nil {
		t.Error("ttfb not measured")
	}
	if record.ResponseBytes <= 0 {
		t.Errorf("response_bytes = %d, want > 0", record.ResponseBytes)
	}
	if record.RequestBytes != int64(len(`{"model":"gpt-4o"}`)) {
		t.Errorf("request_bytes = %d, want %d", record.RequestBytes, len(`{"model":"gpt-4o"}`))
	}
	if record.Model != "gpt-4o" {
		t.Errorf("model = %q, want %q", record.Model, "gpt-4o")
	}
}

func TestRelayPassesQueryString(t *testing.T) {
	upstream := echoUpstream(t, http.StatusOK)
	defer upstream.Close()

	var sink collector
	relay := New(Options{AllowPrivate: true, Timeout: 5 * time.Second}, sink.log, http.HandlerFunc(notFound))

	req := httptest.NewRequest(http.MethodGet, "/"+upstream.URL+"/v1/models?limit=3&api-version=2", nil)
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), `"query":"limit=3&api-version=2"`) {
		t.Errorf("query string not forwarded: %s", rec.Body.String())
	}
}

func TestRelayPreservesUpstreamErrorStatus(t *testing.T) {
	upstream := echoUpstream(t, http.StatusTooManyRequests)
	defer upstream.Close()

	var sink collector
	relay := New(Options{AllowPrivate: true, Timeout: 5 * time.Second}, sink.log, http.HandlerFunc(notFound))

	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL+"/v1/messages", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	record := sink.last(t)
	if !record.IsError || record.StatusCode != http.StatusTooManyRequests {
		t.Errorf("record = %+v, want 429 flagged as error", record)
	}
}

func TestRelayBlocksPrivateTargetsByDefault(t *testing.T) {
	upstream := echoUpstream(t, http.StatusOK)
	defer upstream.Close()

	var sink collector
	relay := New(Options{Timeout: 5 * time.Second}, sink.log, http.HandlerFunc(notFound))

	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL+"/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "blocked address") {
		t.Errorf("expected SSRF block message, got %s", rec.Body.String())
	}
	record := sink.last(t)
	if !record.IsError || !strings.Contains(record.ErrMessage, "blocked address") {
		t.Errorf("record = %+v, want blocked error recorded", record)
	}
}

func TestRelayRejectsTargetWithCredentials(t *testing.T) {
	var sink collector
	relay := New(Options{AllowPrivate: true, Timeout: time.Second}, sink.log, http.HandlerFunc(notFound))

	// Credentials smuggled into the embedded URL would leak upstream.
	req := httptest.NewRequest(http.MethodPost, "/https://user:secret@relay.invalid/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if record := sink.last(t); !record.IsError {
		t.Errorf("record = %+v, want error", record)
	}
}

func TestRelayIgnoresSchemeWithoutTarget(t *testing.T) {
	var sink collector
	relay := New(Options{Timeout: time.Second}, sink.log, http.HandlerFunc(notFound))

	// "/https:" carries no host, so it is not a passthrough request at all and
	// must fall through to the wrapped handler untouched.
	req := httptest.NewRequest(http.MethodGet, "/https:", nil)
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 from the wrapped handler", rec.Code)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.recs) != 0 {
		t.Errorf("non-passthrough request was logged: %+v", sink.recs)
	}
}

func TestRelayDelegatesOtherPaths(t *testing.T) {
	var sink collector
	relay := New(Options{Timeout: time.Second}, sink.log, http.HandlerFunc(notFound))

	req := httptest.NewRequest(http.MethodGet, "/openai/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the wrapped handler to answer 404", rec.Code)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.recs) != 0 {
		t.Errorf("non-passthrough request was logged: %+v", sink.recs)
	}
}

// TestRelayStreamsAsUpstreamEmits proves the relay does not buffer a response:
// the first SSE event must reach the client well before upstream finishes.
func TestRelayStreamsAsUpstreamEmits(t *testing.T) {
	gap := 400 * time.Millisecond
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: first\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(gap)
		fmt.Fprint(w, "data: second\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	var sink collector
	relay := New(Options{AllowPrivate: true, Timeout: 10 * time.Second}, sink.log, http.HandlerFunc(notFound))
	front := httptest.NewServer(relay)
	defer front.Close()

	resp, err := http.Get(front.URL + "/" + upstream.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	started := time.Now()
	var firstAt time.Duration
	lines := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		lines++
		if lines == 1 {
			firstAt = time.Since(started)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if lines != 2 {
		t.Fatalf("received %d events, want 2", lines)
	}
	if firstAt > gap/2 {
		t.Errorf("first event arrived after %v, want well under the %v gap (relay buffered?)", firstAt, gap)
	}

	record := sink.last(t)
	if record.TTFTMs == nil {
		t.Fatal("ttft not measured for a streamed response")
	}
	if *record.TTFTMs > record.TotalMs {
		t.Errorf("ttft %d ms exceeds total %d ms", *record.TTFTMs, record.TotalMs)
	}
	if record.InputTokens != nil || record.OutputTokens != nil {
		t.Errorf("a stream without usage reported tokens: %v/%v", record.InputTokens, record.OutputTokens)
	}
}

// TestRelayRecordsUsageFromSSE wires the capture into the full relay path and
// checks both the recorded tokens and that the client still receives the
// response bytes verbatim.
func TestRelayRecordsUsageFromSSE(t *testing.T) {
	sent := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":17,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, sent)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	var sink collector
	relay := New(Options{AllowPrivate: true, Timeout: 10 * time.Second}, sink.log, http.HandlerFunc(notFound))
	front := httptest.NewServer(relay)
	defer front.Close()

	resp, err := http.Get(front.URL + "/" + upstream.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(got) != sent {
		t.Errorf("client received altered bytes:\n got %q\nwant %q", got, sent)
	}

	record := sink.last(t)
	if record.InputTokens == nil || *record.InputTokens != 17 {
		t.Errorf("recorded input tokens = %v, want 17", record.InputTokens)
	}
	if record.OutputTokens == nil || *record.OutputTokens != 5 {
		t.Errorf("recorded output tokens = %v, want 5", record.OutputTokens)
	}
}

// TestRelayRecordsUsageFromJSONBody covers the non-streaming shape: the echoed
// upstream body carries an OpenAI usage object.
func TestRelayRecordsUsageFromJSONBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl_1","usage":{"prompt_tokens":41,"completion_tokens":9}}`)
	}))
	defer upstream.Close()

	var sink collector
	relay := New(Options{AllowPrivate: true, Timeout: 5 * time.Second}, sink.log, http.HandlerFunc(notFound))

	req := httptest.NewRequest(http.MethodPost, "/"+upstream.URL+"/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	relay.ServeHTTP(rec, req)

	record := sink.last(t)
	if record.InputTokens == nil || *record.InputTokens != 41 {
		t.Errorf("recorded input tokens = %v, want 41", record.InputTokens)
	}
	if record.OutputTokens == nil || *record.OutputTokens != 9 {
		t.Errorf("recorded output tokens = %v, want 9", record.OutputTokens)
	}
}
