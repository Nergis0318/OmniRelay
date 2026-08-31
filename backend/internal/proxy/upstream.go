package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

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

	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		rc.c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt provider key"})
		return nil, time.Time{}, true
	}

	if provider.ProviderType == "gemini" && isStream {
		endpoint = applyGeminiStreamingURL("gemini", endpoint, true)
	}

	adaptedJSON, _ := json.Marshal(adaptedBody)
	modelURL := joinUpstreamURL(provider.APiBaseURL, endpoint)
	req, err := http.NewRequest("POST", modelURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &rc.apiKeyID,
			ProviderID:   &provider.ID,
			Model:        rc.fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &rc.userID,
		})
		rc.c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create the model request"})
		return nil, time.Time{}, true
	}

	req.Header.Set("Content-Type", "application/json")
	copyForwardableRequestHeaders(rc.c, req)
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime = time.Now()
	resp, err = client.Do(req)
	if err != nil {
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
