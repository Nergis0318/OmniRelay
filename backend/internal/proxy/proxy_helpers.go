package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const upstreamRequestTimeout = 5 * time.Minute

// isEmptyResponseBody reports whether an upstream body has no content.
func isEmptyResponseBody(body []byte) bool {
	return len(bytes.TrimSpace(body)) == 0
}

// isUpstreamErrorContent checks if the response text is a known error sent by
// an upstream that does not follow standard error reporting. Both the plain
// and bracketed forms occur in the wild.
func isUpstreamErrorContent(text string) bool {
	text = strings.TrimSpace(text)
	return text == "Empty message" || text == "[Empty message]"
}

// interruptionMarker is the prefix Anthropic uses for temporary service
// interruptions (error type "interruption").
const interruptionMarker = "Temporary service interruption"

const generationFailureMarker = "The response did not generate correctly"

// isInterruptionText reports whether text is Anthropic's temporary service
// interruption message. Prefix match so variants stay detected.
func isInterruptionText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), interruptionMarker)
}

// isGenerationFailureText reports whether text is the upstream generation
// failure message that is sometimes returned as normal content instead of a
// standard error (e.g. "The response did not generate correctly. Please
// resend the last message and I will continue without resetting the session").
func isGenerationFailureText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), generationFailureMarker)
}

// isErrorContent reports whether a response text is a known non-standard
// upstream error embedded in an otherwise successful response.
func isErrorContent(text string) bool {
	return isUpstreamErrorContent(text) || isInterruptionText(text) || isGenerationFailureText(text)
}

// abortErrorContent writes a standard error for upstream error text embedded
// in a successful response. Upstream errors and interruptions are temporary →
// 503 retryable; other cases keep the legacy 200 api_error behavior.
func abortErrorContent(c *gin.Context, errMsg string) {
	errFmt := apiresponse.FormatFromContext(c)
	if isErrorContent(errMsg) {
		apiresponse.AbortServiceUnavailable(c, errFmt, errMsg)
		return
	}
	apiresponse.Abort(c, http.StatusOK, errFmt, "api_error", errMsg, "", "")
}

// extractErrorContent checks a parsed response for non-standard error text embedded in content.
// Returns the error message if found, empty string otherwise.
func extractErrorContent(response map[string]interface{}) string {
	if content, ok := response["content"].([]interface{}); ok {
		for _, c := range content {
			if block, ok := c.(map[string]interface{}); ok {
				if block["type"] == "text" {
					if text, ok := block["text"].(string); ok && isErrorContent(text) {
						return text
					}
				}
			}
		}
	}
	if choices, ok := response["choices"].([]interface{}); ok {
		for _, c := range choices {
			if choice, ok := c.(map[string]interface{}); ok {
				if msg, ok := choice["message"].(map[string]interface{}); ok {
					if text, ok := msg["content"].(string); ok && isErrorContent(text) {
						return text
					}
				}
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if text, ok := delta["content"].(string); ok && isErrorContent(text) {
						return text
					}
				}
			}
		}
	}
	// Responses API format: output[].content[].text
	if output, ok := response["output"].([]interface{}); ok {
		for _, rawItem := range output {
			item, _ := rawItem.(map[string]interface{})
			if item["type"] != "message" {
				continue
			}
			content, _ := item["content"].([]interface{})
			for _, rawPart := range content {
				part, _ := rawPart.(map[string]interface{})
				if text, ok := part["text"].(string); ok && isErrorContent(text) {
					return text
				}
			}
		}
	}
	return ""
}

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

// doUpstream issues a request with the engine's shared HTTP client and returns the response and start time.
func (e *Engine) doUpstream(req *http.Request) (*http.Response, time.Time, error) {
	start := time.Now()
	resp, err := e.httpClient.Do(req)
	return resp, start, err
}

// writeUpstreamErrorBody reads the upstream error body and writes it back to the client,
// normalizing to the client's expected format (OpenAI or Anthropic) and including request_id.
// Falls back to wrapping the raw body as a structured error if it cannot be parsed.
func writeUpstreamErrorBody(c *gin.Context, resp *http.Response, providerType string) {
	respBody, _ := io.ReadAll(resp.Body)

	if parsed, ok := parseUpstreamError(providerType, respBody); ok {
		errFmt := apiresponse.FormatFromContext(c)
		requestID := c.GetString("request_id")
		normalized := reformatError(parsed, errFmt, requestID)
		c.Data(resp.StatusCode, "application/json", normalized)
		return
	}

	errFmt := apiresponse.FormatFromContext(c)
	requestID := c.GetString("request_id")
	msg := strings.TrimSpace(string(respBody))
	if msg == "" {
		msg = fmt.Sprintf("upstream error (HTTP %d)", resp.StatusCode)
	}
	wrapped := reformatError(upstreamError{ErrType: errorTypeForFormat(resp.StatusCode, errFmt), Message: msg}, errFmt, requestID)
	c.Data(resp.StatusCode, "application/json", wrapped)
}

// proxyJSONRequest is a common scaffolding for "marshal adapted body → POST → status check".
// On success it returns (resp, startTime, true). On any failure it writes an error response,
// logs the failure via the engine, and returns ok=false. Callers must close the response body.
func (e *Engine) proxyJSONRequest(c *gin.Context, u usageContext, providerType, apiKey, upstreamURL string, adaptedBody map[string]interface{}, logNewRequestErrors bool) (*http.Response, time.Time, bool) {
	errFmt := apiresponse.FormatFromContext(c)

	adaptedJSON, err := json.Marshal(adaptedBody)
	if err != nil {
		apiresponse.AbortInternal(c, errFmt, "failed to encode upstream request")
		return nil, time.Time{}, false
	}

	req, err := buildUpstreamRequest(c, http.MethodPost, upstreamURL, adaptedJSON, providerType, apiKey)
	if err != nil {
		if logNewRequestErrors {
			e.logUpstreamError(u, err.Error(), 0)
		}
		apiresponse.AbortInternal(c, errFmt, "failed to create upstream request")
		return nil, time.Time{}, false
	}

	resp, start, err := e.doUpstream(req)
	if err != nil {
		e.logUpstreamError(u, err.Error(), 0)
		apiresponse.AbortBadGateway(c, errFmt, fmt.Sprintf("upstream request failed: %v", err))
		return nil, time.Time{}, false
	}

	if !isSuccessStatus(resp.StatusCode) {
		latencyMs := time.Since(start).Milliseconds()
		e.logUpstreamError(u, fmt.Sprintf("upstream returned %d", resp.StatusCode), latencyMs)
		writeUpstreamErrorBody(c, resp, providerType)
		resp.Body.Close()
		return nil, time.Time{}, false
	}

	return resp, start, true
}

// apiFormat identifies the incoming request's wire format, used to pick a
// matching upstream endpoint.
type apiFormat int

const (
	apiFormatOpenAI apiFormat = iota
	apiFormatAnthropic
)

// openaiCompatPriority is the endpoint api_type preference for OpenAI-family
// requests (mirrors isOpenAICompat).
var openaiCompatPriority = []string{"openai", "lmstudio", "ollama"}

// effectiveProvider returns a shallow copy of provider whose ProviderType and
// APiBaseURL are overridden by the endpoint matching the request format, or the
// original provider when no matching endpoint exists.
func effectiveProvider(provider *models.Provider, format apiFormat) *models.Provider {
	types := formatEndpointTypes(format)
	for _, t := range types {
		for i := range provider.Endpoints {
			if provider.Endpoints[i].APIType == t {
				cp := *provider
				cp.ProviderType = t
				cp.APiBaseURL = provider.Endpoints[i].BaseURL
				return &cp
			}
		}
	}
	return provider
}

func formatEndpointTypes(format apiFormat) []string {
	if format == apiFormatAnthropic {
		return []string{"anthropic"}
	}
	return openaiCompatPriority
}
