package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestApplyGeminiStreamingURL(t *testing.T) {
	cases := []struct {
		name         string
		providerType string
		endpoint     string
		isStream     bool
		want         string
	}{
		{
			name:         "no-op for non-gemini",
			providerType: "openai",
			endpoint:     "/v1/chat/completions",
			isStream:     true,
			want:         "/v1/chat/completions",
		},
		{
			name:         "no-op when not streaming",
			providerType: "gemini",
			endpoint:     "/models/gemini-2.0-flash:generateContent",
			isStream:     false,
			want:         "/models/gemini-2.0-flash:generateContent",
		},
		{
			name:         "rewrites to streamGenerateContent and adds alt query",
			providerType: "gemini",
			endpoint:     "/models/gemini-2.0-flash:generateContent",
			isStream:     true,
			want:         "/models/gemini-2.0-flash:streamGenerateContent?alt=sse",
		},
		{
			name:         "appends with & when query already present",
			providerType: "gemini",
			endpoint:     "/models/gemini-2.0-flash:generateContent?key=abc",
			isStream:     true,
			want:         "/models/gemini-2.0-flash:streamGenerateContent?key=abc&alt=sse",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyGeminiStreamingURL(tc.providerType, tc.endpoint, tc.isStream)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildUpstreamRequestSetsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Accept-Language", "ko-KR")

	body := []byte(`{"model":"claude"}`)
	req, err := buildUpstreamRequest(c, http.MethodPost, "https://upstream.example/v1/messages", body, "anthropic", "secret-key")
	if err != nil {
		t.Fatalf("buildUpstreamRequest returned error: %v", err)
	}

	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := req.Header.Get("Accept-Language"); got != "ko-KR" {
		t.Fatalf("Accept-Language not forwarded, got %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "secret-key" {
		t.Fatalf("x-api-key = %q, want secret-key", got)
	}
	if got := req.Header.Get("anthropic-version"); got != defaultAnthropicVersion {
		t.Fatalf("anthropic-version = %q, want %q", got, defaultAnthropicVersion)
	}

	gotBody, _ := io.ReadAll(req.Body)
	if string(gotBody) != string(body) {
		t.Fatalf("body = %q, want %q", gotBody, body)
	}
}

func TestBuildUpstreamRequestNilBodyOmitsContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	req, err := buildUpstreamRequest(c, http.MethodGet, "https://upstream.example/v1/models", nil, "openai", "k")
	if err != nil {
		t.Fatalf("buildUpstreamRequest returned error: %v", err)
	}

	if got := req.Header.Get("Content-Type"); got != "" {
		t.Fatalf("Content-Type should be empty for nil body, got %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer k" {
		t.Fatalf("Authorization = %q, want \"Bearer k\"", got)
	}
}

func TestStripProviderPrefix(t *testing.T) {
	cases := map[string]string{
		"openai/gpt-4o":               "gpt-4o",
		"anthropic/claude-sonnet-4-6": "claude-sonnet-4-6",
		"gpt-4o":                      "gpt-4o",
		"":                            "",
		"openai/gpt-4o/with-extra":    "gpt-4o/with-extra",
		"/leading-slash":              "leading-slash",
	}
	for input, want := range cases {
		if got := stripProviderPrefix(input); got != want {
			t.Errorf("stripProviderPrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSetGenConfigCreatesMap(t *testing.T) {
	body := map[string]interface{}{}
	setGenConfig(body, "temperature", 0.7)

	gc, ok := body["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("generationConfig was not created as map[string]interface{}, got %T", body["generationConfig"])
	}
	if gc["temperature"] != 0.7 {
		t.Fatalf("temperature = %v, want 0.7", gc["temperature"])
	}
}

func TestSetGenConfigMergesIntoExisting(t *testing.T) {
	body := map[string]interface{}{
		"generationConfig": map[string]interface{}{
			"temperature": 0.5,
		},
	}
	setGenConfig(body, "maxOutputTokens", 1024)
	setGenConfig(body, "topP", 0.9)

	gc := body["generationConfig"].(map[string]interface{})
	if gc["temperature"] != 0.5 {
		t.Errorf("temperature lost during merge, got %v", gc["temperature"])
	}
	if gc["maxOutputTokens"] != 1024 {
		t.Errorf("maxOutputTokens = %v, want 1024", gc["maxOutputTokens"])
	}
	if gc["topP"] != 0.9 {
		t.Errorf("topP = %v, want 0.9", gc["topP"])
	}
}

func TestSetGenConfigOverwritesWrongType(t *testing.T) {
	body := map[string]interface{}{
		"generationConfig": "this should not be here",
	}
	setGenConfig(body, "temperature", 0.7)

	gc, ok := body["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("generationConfig should have been replaced with a map; got %T", body["generationConfig"])
	}
	if gc["temperature"] != 0.7 {
		t.Fatalf("temperature = %v, want 0.7", gc["temperature"])
	}
}

func TestApplyGeminiStreamingURLDoesNotAffectAlreadyStreaming(t *testing.T) {
	// If a provider erroneously already passed :streamGenerateContent we shouldn't double-rewrite.
	in := "/models/gemini-2.0-flash:streamGenerateContent"
	out := applyGeminiStreamingURL("gemini", in, true)
	// strings.Replace with count=1 looks for ":generateContent" which is not in the string -> no change.
	// alt=sse is still appended.
	want := in + "?alt=sse"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}

	// Defense-in-depth: a downstream call after this should not re-append alt=sse twice.
	if strings.Count(out, "alt=sse") != 1 {
		t.Fatalf("alt=sse should appear exactly once, got %q", out)
	}
}

func TestWriteUpstreamErrorBodyForwardsStatusAndPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
	}

	writeUpstreamErrorBody(c, resp)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := w.Body.String(); got != `{"error":"rate limited"}` {
		t.Fatalf("body = %q", got)
	}
}
