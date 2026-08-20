package proxy

import (
	"io"
	"net/http"
	"time"

	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

func (e *Engine) executeChat(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID, userID int64) {
	resp, startTime, inputTokens, wroteError := e.buildAndSendChatRequest(c, provider, dbModel, adapter, body, fullModelID, apiKeyID, userID)
	if wroteError {
		return
	}
	defer resp.Body.Close()

	if extractStreamFlag(body) {
		e.handleStreamResponse(c, resp, adapter, apiKeyID, provider.ID, fullModelID, dbModel, userID, provider.ProviderType, inputTokens)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: "failed to read the model response",
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read the model response"})
		return
	}

	finalResponse, wrote := parseNonStreamChatResponse(c, respBody, resp.Header, adapter, fullModelID, dbModel, apiKeyID, provider.ID, startTime, userID, e.usageService, provider.ProviderType, inputTokens)
	if !wrote && finalResponse != nil {
		c.JSON(http.StatusOK, finalResponse)
	}
}

// buildAndSendChatRequest builds the upstream chat request, sends it, and
// returns the upstream response. wroteError is true when an error response
// was already written to c.
func (e *Engine) buildAndSendChatRequest(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID, userID int64) (*http.Response, time.Time, int64, bool) {
	isStream := extractStreamFlag(body)

	if dbModel.ContextWindow > 0 {
		body["_context_window"] = dbModel.ContextWindow
	}

	endpoint, adaptedBody, err := adapter.BuildChatRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, time.Time{}, 0, true
	}

	// Inject stream_options for OpenAI-compatible streaming to get usage data
	if isStream && isOpenAICompat(provider.ProviderType) {
		if _, ok := adaptedBody["stream_options"]; !ok {
			adaptedBody["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}

	// Count input tokens locally before sending to upstream
	inputTokens := countInputTokens(adaptedBody, fullModelID)

	rc := &requestContext{
		c:           c,
		provider:    provider,
		dbModel:     dbModel,
		apiKeyID:    apiKeyID,
		userID:      userID,
		fullModelID: fullModelID,
		engine:      e,
	}

	resp, startTime, wroteError := rc.executeUpstream(adaptedBody, endpoint, isStream)
	if wroteError {
		return nil, time.Time{}, 0, true
	}

	if !isSuccessStatus(resp.StatusCode) {
		latencyMs := time.Since(startTime).Milliseconds()
		logErrorResponse(e, apiKeyID, provider.ID, fullModelID, resp.StatusCode, latencyMs, userID)
		writeUpstreamErrorBody(c, resp, provider.ProviderType)
		resp.Body.Close()
		return nil, time.Time{}, 0, true
	}

	return resp, startTime, inputTokens, false
}
