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
	adapter := adapters[provider.ProviderType]
	if adapter != nil {
		return testViaAdapter(adapter, provider, apiKey, modelID, httpClient)
	}
	return testDirect(provider, apiKey, modelID, httpClient)
}

func testViaAdapter(adapter Adapter, provider *models.Provider, apiKey, modelID string, httpClient *http.Client) TestProviderResult {
	body := map[string]interface{}{
		"model":      modelID,
		"messages":   []map[string]interface{}{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
		"stream":     false,
	}

	endpoint, adaptedBody, err := adapter.BuildChatRequest(body)
	if err != nil {
		return TestProviderResult{Ok: false, Error: err.Error()}
	}

	if isOpenAICompat(provider.ProviderType) {
		if _, ok := adaptedBody["stream_options"]; !ok {
			adaptedBody["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}
	endpoint = applyGeminiStreamingURL(provider.ProviderType, endpoint, false)

	jsonBody, err := json.Marshal(adaptedBody)
	if err != nil {
		return TestProviderResult{Ok: false, Error: err.Error()}
	}

	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)
	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return TestProviderResult{Ok: false, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return TestProviderResult{Ok: false, LatencyMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if !isSuccessStatus(resp.StatusCode) {
		return TestProviderResult{Ok: false, LatencyMs: time.Since(start).Milliseconds(), Error: fmt.Sprintf("upstream returned %d", resp.StatusCode)}
	}
	return TestProviderResult{Ok: true, LatencyMs: time.Since(start).Milliseconds()}
}

func testDirect(provider *models.Provider, apiKey, modelID string, httpClient *http.Client) TestProviderResult {
	body := map[string]interface{}{
		"model":      modelID,
		"messages":   []map[string]interface{}{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
		"stream":     false,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return TestProviderResult{Ok: false, Error: err.Error()}
	}

	upstreamURL := joinUpstreamURL(provider.APiBaseURL, "/chat/completions")
	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return TestProviderResult{Ok: false, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return TestProviderResult{Ok: false, LatencyMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if !isSuccessStatus(resp.StatusCode) {
		return TestProviderResult{Ok: false, LatencyMs: time.Since(start).Milliseconds(), Error: fmt.Sprintf("upstream returned %d", resp.StatusCode)}
	}
	return TestProviderResult{Ok: true, LatencyMs: time.Since(start).Milliseconds()}
}
