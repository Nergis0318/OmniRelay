package proxy

import (
	"io"
	"net/http"
	"time"

	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

func (e *Engine) executeChat(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID, userID int64) {
	isStream := extractStreamFlag(body)

	if dbModel.ContextWindow > 0 {
		body["_context_window"] = dbModel.ContextWindow
	}

	endpoint, adaptedBody, err := adapter.BuildChatRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
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
		return
	}
	defer resp.Body.Close()

	if !isSuccessStatus(resp.StatusCode) {
		respBody, _ := io.ReadAll(resp.Body)
		latencyMs := time.Since(startTime).Milliseconds()
		logErrorResponse(e, apiKeyID, provider.ID, fullModelID, resp.StatusCode, latencyMs, userID)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	if isStream {
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

	handleNonStreamChatResponse(c, respBody, resp.Header, adapter, fullModelID, dbModel, apiKeyID, provider.ID, startTime, userID, e.usageService, provider.ProviderType, inputTokens)
}
