package proxy

import (
	"net/http"
	"strings"
	"time"

	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

func (e *Engine) handleStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, userID int64) {
	c.Status(http.StatusOK)
	copyResponseHeaders(c, resp.Header)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	start := time.Now()
	buf := make([]byte, 4096)

	var totalInputTokens, totalOutputTokens int64
	sentDone := false

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if strings.Contains(string(chunk), "data: [DONE]") {
				sentDone = true
			}

			transformed, inTok, outTok, _ := adapter.ParseStreamChunk(chunk)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}
			if transformed != nil {
				if strings.Contains(string(transformed), "data: [DONE]") {
					sentDone = true
				}
				c.Writer.Write(transformed)
				flusher.Flush()
			} else {
				c.Writer.Write(chunk)
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	if !sentDone {
		c.Writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}

	latencyMs := time.Since(start).Milliseconds()
	completedAt := time.Now()

	cost := calculateCost(dbModel, totalInputTokens, totalOutputTokens, 0, 0, 0)

	e.usageService.Log(models.UsageLog{
		APIKeyID:       &apiKeyID,
		ProviderID:     &providerID,
		Model:          fullModelID,
		RequestTokens:  totalInputTokens,
		ResponseTokens: totalOutputTokens,
		TotalTokens:    totalInputTokens + totalOutputTokens,
		LatencyMs:      latencyMs,
		Cost:           cost,
		StartedAt:      &start,
		CompletedAt:    &completedAt,
		UserID:         &userID,
	})
}

func (e *Engine) handleRawStreamResponse(c *gin.Context, resp *http.Response, apiKeyID, providerID int64, fullModelID string, start time.Time, userID int64) {
	c.Status(http.StatusOK)
	copyResponseHeaders(c, resp.Header)
	if resp.Header.Get("Content-Type") == "" {
		c.Header("Content-Type", "text/event-stream")
	}
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			c.Writer.Write(buf[:n])
			flusher.Flush()
		}
		if err != nil {
			break
		}
	}

	e.usageService.Log(models.UsageLog{
		APIKeyID:   &apiKeyID,
		ProviderID: &providerID,
		Model:      fullModelID,
		LatencyMs:  time.Since(start).Milliseconds(),
		UserID:     &userID,
	})
}

func (e *Engine) handleMessagesStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, start time.Time, userID int64) {
	c.Status(http.StatusOK)
	copyResponseHeaders(c, resp.Header)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	buf := make([]byte, 4096)
	state := make(map[string]interface{})
	var totalInputTokens, totalOutputTokens int64

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			transformed, inTok, outTok, _ := adapter.ParseMessagesStreamChunk(buf[:n], state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}
			if transformed != nil {
				c.Writer.Write(transformed)
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	completedAt := time.Now()
	log := models.UsageLog{
		APIKeyID:       &apiKeyID,
		ProviderID:     &providerID,
		Model:          fullModelID,
		RequestTokens:  totalInputTokens,
		ResponseTokens: totalOutputTokens,
		TotalTokens:    totalInputTokens + totalOutputTokens,
		LatencyMs:      time.Since(start).Milliseconds(),
		StartedAt:      &start,
		CompletedAt:    &completedAt,
		UserID:         &userID,
	}
	if dbModel != nil && (totalInputTokens > 0 || totalOutputTokens > 0) {
		log.Cost = calculateCost(dbModel, totalInputTokens, totalOutputTokens, 0, 0, 0)
	}
	e.usageService.Log(log)
}
