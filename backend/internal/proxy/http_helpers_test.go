package proxy

import (
	"encoding/json"
	"net/http"
	"testing"
)

func newReqForHeaders() *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "https://upstream.example/", nil)
	return req
}

func TestRouteAPIPrefix(t *testing.T) {
	cases := map[string]string{
		"/openai/v1/chat/completions":     "v1",
		"/anthropic/v1/messages":          "v1",
		"/gemini/v1beta/models":           "v1beta",
		"/ollama/api/tags":                "api",
		"/lmstudio/v1":                    "v1",
		"/justprovider":                   "",
		"":                                "",
	}
	for input, want := range cases {
		if got := routeAPIPrefix(input); got != want {
			t.Errorf("routeAPIPrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRoutedBaseURLOllamaAPIPrefix(t *testing.T) {
	// Ollama with apiPrefix=api should strip a trailing /v1 from the base URL.
	if got := routedBaseURL("ollama", "api", "http://host:11434/v1"); got != "http://host:11434" {
		t.Errorf("ollama+api should strip /v1: got %q", got)
	}
	// Other combinations leave the base URL untouched.
	if got := routedBaseURL("ollama", "v1", "http://host:11434/v1"); got != "http://host:11434/v1" {
		t.Errorf("ollama+v1 should not strip: got %q", got)
	}
	if got := routedBaseURL("openai", "api", "https://api.openai.com/v1"); got != "https://api.openai.com/v1" {
		t.Errorf("non-ollama should be untouched: got %q", got)
	}
}

func TestRoutedEndpoint(t *testing.T) {
	// Empty endpoint defaults to "/".
	if got := routedEndpoint("v1", "https://api.openai.com", ""); got != "/v1/" {
		t.Errorf("empty endpoint: got %q, want /v1/", got)
	}
	// Endpoint without leading slash gets one.
	if got := routedEndpoint("v1", "https://api.openai.com", "chat/completions"); got != "/v1/chat/completions" {
		t.Errorf("missing slash: got %q, want /v1/chat/completions", got)
	}
	// If the base URL already ends with /<apiPrefix> the prefix is not duplicated.
	if got := routedEndpoint("v1", "https://api.openai.com/v1", "/chat/completions"); got != "/chat/completions" {
		t.Errorf("base already has prefix: got %q, want /chat/completions", got)
	}
	// Empty apiPrefix passes the endpoint through.
	if got := routedEndpoint("", "https://host", "/something"); got != "/something" {
		t.Errorf("empty prefix: got %q, want /something", got)
	}
}

func TestAppendRawQuery(t *testing.T) {
	if got := appendRawQuery("https://x/y", ""); got != "https://x/y" {
		t.Errorf("empty query: got %q", got)
	}
	if got := appendRawQuery("https://x/y", "a=1"); got != "https://x/y?a=1" {
		t.Errorf("first query: got %q, want ?a=1", got)
	}
	if got := appendRawQuery("https://x/y?key=k", "a=1"); got != "https://x/y?key=k&a=1" {
		t.Errorf("additional query: got %q, want &a=1", got)
	}
}

func TestExtractUsageFromRawResponseAnthropic(t *testing.T) {
	body := map[string]interface{}{
		"usage": map[string]interface{}{
			"input_tokens":  float64(11),
			"output_tokens": float64(4),
		},
	}
	rq, rs, total, _, _, _ := extractUsageFromRawResponse("anthropic", body)
	if rq != 11 || rs != 4 {
		t.Errorf("anthropic tokens = (%d, %d), want (11, 4)", rq, rs)
	}
	if total != 15 {
		t.Errorf("total should derive when missing: got %d, want 15", total)
	}
}

func TestExtractUsageFromRawResponseGemini(t *testing.T) {
	body := map[string]interface{}{
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(3),
			"candidatesTokenCount": float64(2),
			"totalTokenCount":      float64(5),
		},
	}
	rq, rs, total, _, _, _ := extractUsageFromRawResponse("gemini", body)
	if rq != 3 || rs != 2 || total != 5 {
		t.Errorf("gemini tokens = (%d, %d, %d), want (3, 2, 5)", rq, rs, total)
	}
}

func TestExtractUsageFromRawResponseOpenAIWithCache(t *testing.T) {
	body := map[string]interface{}{
		"usage": map[string]interface{}{
			"prompt_tokens":               float64(10),
			"completion_tokens":           float64(20),
			"total_tokens":                float64(30),
			"cache_creation_input_tokens": float64(4),
			"cache_read_input_tokens":     float64(6),
		},
	}
	rq, rs, total, cw5m, _, cr := extractUsageFromRawResponse("openai", body)
	if rq != 10 || rs != 20 || total != 30 {
		t.Errorf("openai tokens = (%d, %d, %d), want (10, 20, 30)", rq, rs, total)
	}
	if cw5m != 4 || cr != 6 {
		t.Errorf("cache tokens = (write5m=%d, read=%d), want (4, 6)", cw5m, cr)
	}
}

func TestNumberToInt64HandlesAllSupportedTypes(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
		want  int64
	}{
		{"float64", float64(42.7), 42},
		{"int", int(7), 7},
		{"int64", int64(99), 99},
		{"json.Number", json.Number("12345"), 12345},
		{"string", "not a number", 0},
		{"nil", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := numberToInt64(tc.value); got != tc.want {
				t.Errorf("numberToInt64(%v) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestIsJSONContentType(t *testing.T) {
	cases := map[string]bool{
		"":                                 true, // empty content type is treated as JSON
		"application/json":                 true,
		"application/json; charset=utf-8":  true,
		"APPLICATION/JSON":                 true,
		"text/plain":                       false,
		"text/event-stream":                false,
	}
	for input, want := range cases {
		if got := isJSONContentType(input); got != want {
			t.Errorf("isJSONContentType(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestSetProviderAuthHeaders(t *testing.T) {
	// anthropic: x-api-key + default anthropic-version when missing
	req := newReqForHeaders()
	setProviderAuthHeaders(req, "anthropic", "secret")
	if got := req.Header.Get("x-api-key"); got != "secret" {
		t.Errorf("anthropic x-api-key = %q, want secret", got)
	}
	if got := req.Header.Get("anthropic-version"); got != defaultAnthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", got, defaultAnthropicVersion)
	}

	// anthropic: existing anthropic-version is preserved (not overwritten)
	req = newReqForHeaders()
	req.Header.Set("anthropic-version", "2024-01-01")
	setProviderAuthHeaders(req, "anthropic", "k")
	if got := req.Header.Get("anthropic-version"); got != "2024-01-01" {
		t.Errorf("existing anthropic-version should be preserved, got %q", got)
	}

	// gemini: x-goog-api-key only
	req = newReqForHeaders()
	setProviderAuthHeaders(req, "gemini", "g-key")
	if got := req.Header.Get("x-goog-api-key"); got != "g-key" {
		t.Errorf("gemini x-goog-api-key = %q, want g-key", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("gemini should not set Authorization, got %q", got)
	}

	// ollama: no auth header
	req = newReqForHeaders()
	setProviderAuthHeaders(req, "ollama", "ignored")
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("ollama should not set Authorization, got %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "" {
		t.Errorf("ollama should not set x-api-key, got %q", got)
	}

	// default (openai/lmstudio): Bearer token
	req = newReqForHeaders()
	setProviderAuthHeaders(req, "openai", "o-key")
	if got := req.Header.Get("Authorization"); got != "Bearer o-key" {
		t.Errorf("openai Authorization = %q, want \"Bearer o-key\"", got)
	}
}
