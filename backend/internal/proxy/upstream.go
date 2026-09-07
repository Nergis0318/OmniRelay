package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

func failoverStatus(err error, status int) bool {
	if err != nil {
		return true
	}
	switch status {
	case 401, 403, 429:
		return true
	}
	return status >= 500
}

func (e *Engine) tryKeys(provider *models.Provider, fn func(key service.UpstreamKey) (*http.Response, time.Time, error)) (*http.Response, time.Time, error) {
	keys, err := e.providerService.ListActiveKeys(provider)
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(keys) == 0 {
		return nil, time.Time{}, fmt.Errorf("failed to decrypt provider key")
	}
	startIdx := e.providerService.NextStartIndex(provider.ID, len(keys))
	var lastResp *http.Response
	var lastStart time.Time
	var lastErr error
	for i := 0; i < len(keys); i++ {
		key := keys[(startIdx+i)%len(keys)]
		resp, start, err := fn(key)
		if lastResp != nil && lastResp != resp {
			io.Copy(io.Discard, lastResp.Body)
			lastResp.Body.Close()
		}
		lastResp, lastStart, lastErr = resp, start, err
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if !failoverStatus(err, status) {
			return resp, start, err
		}
		if status == 401 || status == 403 {
			_ = e.providerService.DeactivateKey(key.ID)
		}
	}
	return lastResp, lastStart, lastErr
}

type requestContext struct {
	c           *gin.Context
	provider    *models.Provider
	dbModel     *models.Model
	apiKeyID    int64
	userID      int64
	fullModelID string
	engine      *Engine
}

func (rc *requestContext) executeUpstream(adaptedBody map[string]interface{}, endpoint string, isStream bool) (resp *http.Response, startTime time.Time, wroteError bool) {
	provider := rc.provider
	e := rc.engine
	if provider.ProviderType == "gemini" && isStream {
		endpoint = applyGeminiStreamingURL("gemini", endpoint, true)
	}
	adaptedJSON, _ := json.Marshal(adaptedBody)
	modelURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	resp, startTime, err := e.tryKeys(provider, func(key service.UpstreamKey) (*http.Response, time.Time, error) {
		req, err := http.NewRequest("POST", modelURL, bytes.NewReader(adaptedJSON))
		if err != nil {
			return nil, time.Time{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		copyForwardableRequestHeaders(rc.c, req)
		setProviderAuthHeaders(req, provider.ProviderType, key.Plaintext)
		client := &http.Client{Timeout: 5 * time.Minute}
		start := time.Now()
		resp, err := client.Do(req)
		return resp, start, err
	})
	if err != nil && resp == nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &rc.apiKeyID,
			ProviderID:   &provider.ID,
			Model:        rc.fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &rc.userID,
		})
		rc.c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("the model request failed: %v", err)})
		return nil, startTime, true
	}
	return resp, startTime, false
}

func logErrorResponse(e *Engine, apiKeyID, providerID int64, fullModelID string, statusCode int, latencyMs int64, userID int64) {
	e.usageService.Log(models.UsageLog{
		APIKeyID:     &apiKeyID,
		ProviderID:   &providerID,
		Model:        fullModelID,
		IsError:      true,
		ErrorMessage: fmt.Sprintf("the model returned %d", statusCode),
		LatencyMs:    latencyMs,
		UserID:       &userID,
	})
}

// parseNonStreamChatResponse parses an upstream chat completions response,
// logs usage, writes the final chat-format response to c, and returns it.
// The bool is true when an error response was already written to c (nothing
// to send).
func parseNonStreamChatResponse(c *gin.Context, respBody []byte, respHeader http.Header, adapter Adapter, fullModelID string, dbModel *models.Model, apiKeyID, providerID int64, startTime time.Time, userID int64, usageService *service.UsageService, providerType string, inputTokens int64, reqBody ...map[string]interface{}) (map[string]interface{}, bool) {
	if isEmptyResponseBody(respBody) {
		latencyMs := time.Since(startTime).Milliseconds()
		usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &providerID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: "the model returned an empty response",
			LatencyMs:    latencyMs,
			UserID:       &userID,
		})
		apiresponse.AbortBadGateway(c, apiresponse.FormatFromContext(c), "the model returned an empty response")
		return nil, true
	}

	var modelResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &modelResponse); err != nil {
		c.Data(http.StatusOK, contentTypeOrDefault(respHeader), respBody)
		return nil, true
	}

	finalResponse, err := adapter.ParseChatResponse(modelResponse)
	if err != nil {
		c.Data(http.StatusOK, contentTypeOrDefault(respHeader), respBody)
		return nil, true
	}

	var fixBody map[string]interface{}
	if len(reqBody) > 0 {
		fixBody = reqBody[0]
	}
	tryFixOpenAIChatResponse(finalResponse, fixBody)

	latencyMs := time.Since(startTime).Milliseconds()

	// Upstream errors embedded in a successful response (e.g. Anthropic
	// interruptions) become standard errors.
	if errMsg := extractErrorContent(finalResponse); errMsg != "" {
		usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &providerID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: errMsg,
			LatencyMs:    latencyMs,
			UserID:       &userID,
		})
		abortErrorContent(c, errMsg)
		return nil, true
	}

	finalResponse["model"] = fullModelID

	// Count output tokens locally from the response content
	localOutput := countOutputTokens(finalResponse, providerType, fullModelID)
	completedAt := time.Now()

	// Extract upstream token data for cache tokens which we can't count locally
	var cacheWrite5m, cacheWrite1h, cacheReadTokens int64
	var upstreamReqTokens, upstreamRespTokens int64
	if usage, ok := finalResponse["usage"].(map[string]interface{}); ok {
		upstreamReqTokens = numberToInt64(usage["prompt_tokens"])
		upstreamRespTokens = numberToInt64(usage["completion_tokens"])
		cacheWrite5m, cacheWrite1h, cacheReadTokens = extractCacheTokens(usage)
	}

	reqTokens := upstreamReqTokens
	if reqTokens == 0 {
		reqTokens = inputTokens
	}
	respTokens := upstreamRespTokens
	if respTokens == 0 {
		respTokens = localOutput
	}
	totalTokens := reqTokens + respTokens

	if reqTokens > 0 || respTokens > 0 || cacheWrite5m > 0 || cacheWrite1h > 0 || cacheReadTokens > 0 {
		cost := calculateCost(dbModel, reqTokens, respTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)

		usageService.Log(models.UsageLog{
			APIKeyID:           &apiKeyID,
			ProviderID:         &providerID,
			Model:              fullModelID,
			RequestTokens:      reqTokens,
			ResponseTokens:     respTokens,
			TotalTokens:        totalTokens,
			CacheWrite5MTokens: cacheWrite5m,
			CacheWrite1HTokens: cacheWrite1h,
			CacheReadTokens:    cacheReadTokens,
			LatencyMs:          latencyMs,
			Cost:               cost,
			StartedAt:          &startTime,
			CompletedAt:        &completedAt,
			UserID:             &userID,
		})
	}

	return finalResponse, false
}

func extractStreamFlag(body map[string]interface{}) bool {
	if stream, ok := body["stream"].(bool); ok {
		return stream
	}
	return false
}
