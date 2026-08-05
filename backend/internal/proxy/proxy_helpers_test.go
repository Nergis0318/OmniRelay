package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"omnirelay/internal/models"
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
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`)),
	}

	writeUpstreamErrorBody(c, resp, "unknown")

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var respBody map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	errObj, ok := respBody["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", respBody)
	}
	if errObj["message"] != "rate limited" {
		t.Errorf("message = %v, want 'rate limited'", errObj["message"])
	}
	if errObj["type"] != "rate_limit_error" {
		t.Errorf("type = %v, want 'rate_limit_error'", errObj["type"])
	}
}

func TestWriteUpstreamErrorBody_UnparseableBody_Anthropic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("Request failed.")),
	}

	writeUpstreamErrorBody(c, resp, "anthropic")

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var respBody map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if respBody["type"] != "error" {
		t.Fatalf("type = %v, want 'error'", respBody["type"])
	}
	errObj, ok := respBody["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", respBody)
	}
	if errObj["message"] != "Request failed." {
		t.Errorf("message = %v, want 'Request failed.'", errObj["message"])
	}
	if errObj["type"] != "api_error" {
		t.Errorf("error.type = %v, want 'api_error'", errObj["type"])
	}
}

func TestWriteUpstreamErrorBody_UnparseableBody_OpenAI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("Request failed.")),
	}

	writeUpstreamErrorBody(c, resp, "openai")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	var respBody map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	errObj, ok := respBody["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", respBody)
	}
	if errObj["message"] != "Request failed." {
		t.Errorf("message = %v, want 'Request failed.'", errObj["message"])
	}
	if errObj["type"] != "server_error" {
		t.Errorf("error.type = %v, want 'server_error'", errObj["type"])
	}
}

func TestWriteUpstreamErrorBody_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}

	writeUpstreamErrorBody(c, resp, "anthropic")

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	var respBody map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if respBody["type"] != "error" {
		t.Fatalf("type = %v, want 'error'", respBody["type"])
	}
	errObj, ok := respBody["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", respBody)
	}
	if errObj["message"] != "upstream error (HTTP 502)" {
		t.Errorf("message = %v, want 'upstream error (HTTP 502)'", errObj["message"])
	}
}

func TestIsInterruptionText(t *testing.T) {
	msg := "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."
	if !isInterruptionText(msg) {
		t.Error("exact message should match")
	}
	if !isInterruptionText("  " + msg + "  ") {
		t.Error("should be whitespace-tolerant")
	}
	if !isInterruptionText("Temporary service interruption.") {
		t.Error("prefix should match")
	}
	if isInterruptionText("Request failed.") {
		t.Error("other text must not match")
	}
	if isInterruptionText("") {
		t.Error("empty must not match")
	}
}

func TestExtractErrorContentInterruption(t *testing.T) {
	msg := "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."
	anthropic := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": msg},
		},
	}
	if got := extractErrorContent(anthropic); !isInterruptionText(got) {
		t.Errorf("anthropic content block: extractErrorContent = %q", got)
	}
	openai := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{"message": map[string]interface{}{"content": msg}},
		},
	}
	if got := extractErrorContent(openai); !isInterruptionText(got) {
		t.Errorf("choices message: extractErrorContent = %q", got)
	}
	if got := extractErrorContent(map[string]interface{}{
		"content": []interface{}{map[string]interface{}{"type": "text", "text": "normal reply"}},
	}); got != "" {
		t.Errorf("normal text: extractErrorContent = %q, want empty", got)
	}
}

func TestAbortErrorContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	msg := "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("request_id", "req-1")
	abortErrorContent(c, msg)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("interruption status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("interruption body = %s", w.Body.String())
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	abortErrorContent(c2, "Empty message")
	if w2.Code != http.StatusOK {
		t.Errorf("legacy status = %d, want 200", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), `"type":"api_error"`) {
		t.Errorf("legacy body = %s", w2.Body.String())
	}
}

func TestEffectiveProvider(t *testing.T) {
	base := &models.Provider{
		ProviderKey:  "gw",
		ProviderType: "openai",
		APiBaseURL:   "https://default.example/v1",
		Endpoints: []models.ProviderEndpoint{
			{APIType: "anthropic", BaseURL: "https://anthropic.example"},
		},
	}

	// OpenAI-family request falls back to the default (no openai/lmstudio/ollama endpoint)
	if got := effectiveProvider(base, apiFormatOpenAI); got != base {
		t.Errorf("openai family: expected original provider, got %+v", got)
	}

	// Anthropic request uses the anthropic endpoint
	got := effectiveProvider(base, apiFormatAnthropic)
	if got == base {
		t.Fatal("anthropic family: expected a copy, got original")
	}
	if got.ProviderType != "anthropic" || got.APiBaseURL != "https://anthropic.example" {
		t.Errorf("anthropic copy = %s / %s", got.ProviderType, got.APiBaseURL)
	}
	// Original is unchanged
	if base.ProviderType != "openai" || base.APiBaseURL != "https://default.example/v1" {
		t.Errorf("original mutated: %s / %s", base.ProviderType, base.APiBaseURL)
	}
}

func TestEffectiveProviderOpenAIPriority(t *testing.T) {
	p := &models.Provider{
		ProviderType: "gemini",
		APiBaseURL:   "https://gemini.example",
		Endpoints: []models.ProviderEndpoint{
			{APIType: "ollama", BaseURL: "http://ollama.local"},
			{APIType: "openai", BaseURL: "https://openai.example"},
		},
	}
	got := effectiveProvider(p, apiFormatOpenAI)
	if got.ProviderType != "openai" || got.APiBaseURL != "https://openai.example" {
		t.Errorf("openai priority = %s / %s", got.ProviderType, got.APiBaseURL)
	}
}

func TestEffectiveProviderNoEndpoints(t *testing.T) {
	p := &models.Provider{ProviderType: "openai", APiBaseURL: "https://default.example/v1"}
	if got := effectiveProvider(p, apiFormatAnthropic); got != p {
		t.Errorf("no endpoints: expected original, got %+v", got)
	}
}
