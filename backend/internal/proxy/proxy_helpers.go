package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const upstreamRequestTimeout = 5 * time.Minute

// applyGeminiStreamingURL rewrites a Gemini endpoint for streaming SSE responses.
// It is a no-op for any non-Gemini provider or when isStream is false.
func applyGeminiStreamingURL(providerType, endpoint string, isStream bool) string {
	if providerType != "gemini" || !isStream {
		return endpoint
	}
	endpoint = strings.Replace(endpoint, ":generateContent", ":streamGenerateContent", 1)
	if strings.Contains(endpoint, "?") {
		endpoint += "&alt=sse"
	} else {
		endpoint += "?alt=sse"
	}
	return endpoint
}

// buildUpstreamRequest assembles a POST request to the upstream provider with the standard
// header set (Content-Type, forwarded client headers, provider-specific auth).
func buildUpstreamRequest(c *gin.Context, method, upstreamURL string, body []byte, providerType, apiKey string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, upstreamURL, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	copyForwardableRequestHeaders(c, req)
	setProviderAuthHeaders(req, providerType, apiKey)
	return req, nil
}

// doUpstream issues a request with the shared HTTP client timeout and returns the response and start time.
func doUpstream(req *http.Request) (*http.Response, time.Time, error) {
	client := &http.Client{Timeout: upstreamRequestTimeout}
	start := time.Now()
	resp, err := client.Do(req)
	return resp, start, err
}

// readUpstreamError reads the body and writes an error response back to the client with the provider's status code.
func writeUpstreamErrorBody(c *gin.Context, resp *http.Response) {
	respBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
}

// proxyJSONRequest is a common scaffolding for "marshal adapted body → POST → status check".
// On success it returns (resp, startTime, true). On any failure it writes an error response,
// logs the failure via the engine, and returns ok=false. Callers must close the response body.
func (e *Engine) proxyJSONRequest(c *gin.Context, u usageContext, providerType, apiKey, upstreamURL string, adaptedBody map[string]interface{}, logNewRequestErrors bool) (*http.Response, time.Time, bool) {
	adaptedJSON, err := json.Marshal(adaptedBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode upstream request"})
		return nil, time.Time{}, false
	}

	req, err := buildUpstreamRequest(c, http.MethodPost, upstreamURL, adaptedJSON, providerType, apiKey)
	if err != nil {
		if logNewRequestErrors {
			e.logUpstreamError(u, err.Error(), 0)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return nil, time.Time{}, false
	}

	resp, start, err := doUpstream(req)
	if err != nil {
		e.logUpstreamError(u, err.Error(), 0)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return nil, time.Time{}, false
	}

	if !isSuccessStatus(resp.StatusCode) {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		latencyMs := time.Since(start).Milliseconds()
		e.logUpstreamError(u, fmt.Sprintf("upstream returned %d", resp.StatusCode), latencyMs)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return nil, time.Time{}, false
	}

	return resp, start, true
}
