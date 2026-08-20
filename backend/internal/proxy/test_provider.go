package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"omnirelay/internal/models"
	"time"
)

// TestProviderResult holds the outcome of a test request sent to a provider.
type TestProviderResult struct {
	Ok        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// TestProvider sends a minimal chat completion request to a provider using its
// first available model and the given API key. It returns the result without
// writing any response to the client.
func TestProvider(provider *models.Provider, apiKey, modelID string, adapters map[string]Adapter, httpClient *http.Client) TestProviderResult {
	body := map[string]interface{}{
		"model":      modelID,
		"messages":   []map[string]interface{}{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
		"stream":     false,
	}
	endpoint := "/chat/completions"

	if adapter := adapters[provider.ProviderType]; adapter != nil {
		var err error
		endpoint, body, err = adapter.BuildChatRequest(body)
		if err != nil {
			return TestProviderResult{Ok: false, Error: err.Error()}
		}
		if isOpenAICompat(provider.ProviderType) {
			if _, ok := body["stream_options"]; !ok {
				body["stream_options"] = map[string]interface{}{"include_usage": true}
			}
		}
		endpoint = applyGeminiStreamingURL(provider.ProviderType, endpoint, false)
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return TestProviderResult{Ok: false, Error: err.Error()}
	}

	req, err := http.NewRequest(http.MethodPost, joinUpstreamURL(provider.APiBaseURL, endpoint), bytes.NewReader(jsonBody))
	if err != nil {
		return TestProviderResult{Ok: false, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return TestProviderResult{Ok: false, LatencyMs: latency, Error: err.Error()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if !isSuccessStatus(resp.StatusCode) {
		return TestProviderResult{Ok: false, LatencyMs: latency, Error: fmt.Sprintf("upstream returned %d", resp.StatusCode)}
	}
	return TestProviderResult{Ok: true, LatencyMs: latency}
}
