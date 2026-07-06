package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"omnirelay/internal/models"

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
		endpoint = applyGeminiStreamOverrides(endpoint)
	}

	adaptedJSON, _ := json.Marshal(adaptedBody)
	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &rc.apiKeyID,
			ProviderID:   &provider.ID,
			Model:        rc.fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &rc.userID,
		})
		rc.c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
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
		rc.c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return nil, startTime, true
	}
	return resp, startTime, false
}

func applyGeminiStreamOverrides(endpoint string) string {
	endpoint = strings.Replace(endpoint, ":generateContent", ":streamGenerateContent", 1)
	if strings.Contains(endpoint, "?") {
		endpoint += "&alt=sse"
	} else {
		endpoint += "?alt=sse"
	}
	return endpoint
}

func logErrorResponse(e *Engine, apiKeyID, providerID int64, fullModelID string, statusCode int, latencyMs int64, userID int64) {
	e.usageService.Log(models.UsageLog{
		APIKeyID:     &apiKeyID,
		ProviderID:   &providerID,
		Model:        fullModelID,
		IsError:      true,
		ErrorMessage: fmt.Sprintf("upstream returned %d", statusCode),
		LatencyMs:    latencyMs,
		UserID:       &userID,
	})
}

func handleNonStreamChatResponse(c *gin.Context, respBody []byte, respHeader http.Header, adapter Adapter, fullModelID string, dbModel *models.Model, apiKeyID, providerID int64, startTime time.Time, userID int64, usageService UsageLogger) {
	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		c.Data(http.StatusOK, contentTypeOrDefault(respHeader), respBody)
		return
	}

	finalResponse, err := adapter.ParseChatResponse(upstreamResponse)
	if err != nil {
		c.Data(http.StatusOK, contentTypeOrDefault(respHeader), respBody)
		return
	}

	finalResponse["model"] = fullModelID

	if usage, ok := finalResponse["usage"].(map[string]interface{}); ok {
		requestTokens := numberToInt64(usage["prompt_tokens"])
		responseTokens := numberToInt64(usage["completion_tokens"])
		totalTokens := numberToInt64(usage["total_tokens"])
		cacheWrite5m, cacheWrite1h, cacheReadTokens := extractCacheTokens(usage)

		cost := calculateCost(dbModel, requestTokens, responseTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
		latencyMs := time.Since(startTime).Milliseconds()
		completedAt := time.Now()

		usageService.Log(models.UsageLog{
			APIKeyID:           &apiKeyID,
			ProviderID:         &providerID,
			Model:              fullModelID,
			RequestTokens:      requestTokens,
			ResponseTokens:     responseTokens,
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

	c.JSON(http.StatusOK, finalResponse)
}

func readBodyAndParse(c *gin.Context) (map[string]interface{}, error) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return nil, err
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return nil, err
	}
	return body, nil
}

func extractStreamFlag(body map[string]interface{}) bool {
	if stream, ok := body["stream"].(bool); ok {
		return stream
	}
	return false
}

type UsageLogger interface {
	Log(log models.UsageLog) error
}
