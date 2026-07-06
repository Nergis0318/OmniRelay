package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

func (e *Engine) handleStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, userID int64, providerType string, inputTokens int64) {
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
	state := make(map[string]interface{})

	var totalInputTokens, totalOutputTokens int64
	var outputTextAccum strings.Builder
	sentDone := false

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			chunkStr := string(chunk)
			if strings.Contains(chunkStr, "data: [DONE]") {
				sentDone = true
			}

			// Track output text for local token counting
			extractDeltaContent(chunkStr, providerType, &outputTextAccum)

			transformed, inTok, outTok, _ := adapter.ParseStreamChunk(chunk, state)
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

	// Prefer locally counted output tokens over upstream values
	if totalOutputTokens == 0 && outputTextAccum.Len() > 0 {
		totalOutputTokens = countTextTokens(outputTextAccum.String(), fullModelID)
	}
	// Prefer locally counted input tokens over upstream values
	if inputTokens > 0 {
		totalInputTokens = inputTokens
	}

	cacheWrite5m := int64State(state, "cache_write_5m_tokens", 0)
	cacheWrite1h := int64State(state, "cache_write_1h_tokens", 0)
	cacheReadTokens := int64State(state, "cache_read_tokens", 0)
	var cost float64
	if dbModel != nil && (totalInputTokens > 0 || totalOutputTokens > 0) {
		cost = calculateCost(dbModel, totalInputTokens, totalOutputTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
	}

	e.usageService.Log(models.UsageLog{
		APIKeyID:           &apiKeyID,
		ProviderID:         &providerID,
		Model:              fullModelID,
		RequestTokens:      totalInputTokens,
		ResponseTokens:     totalOutputTokens,
		TotalTokens:        totalInputTokens + totalOutputTokens,
		CacheWrite5MTokens: cacheWrite5m,
		CacheWrite1HTokens: cacheWrite1h,
		CacheReadTokens:    cacheReadTokens,
		LatencyMs:          latencyMs,
		Cost:               cost,
		StartedAt:          &start,
		CompletedAt:        &completedAt,
		UserID:             &userID,
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

func (e *Engine) handleMessagesStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, start time.Time, userID int64, providerType string, inputTokens int64) {
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
	var outputTextAccum strings.Builder

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			// Track output text for local token counting
			extractDeltaContent(string(chunk), providerType, &outputTextAccum)

			transformed, inTok, outTok, _ := adapter.ParseMessagesStreamChunk(chunk, state)
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

	// Prefer locally counted output tokens over upstream values
	if totalOutputTokens == 0 && outputTextAccum.Len() > 0 {
		totalOutputTokens = countTextTokens(outputTextAccum.String(), fullModelID)
	}
	// Prefer locally counted input tokens over upstream values
	if inputTokens > 0 {
		totalInputTokens = inputTokens
	}

	cacheWrite5m := int64State(state, "cache_write_5m_tokens", 0)
	cacheWrite1h := int64State(state, "cache_write_1h_tokens", 0)
	cacheReadTokens := int64State(state, "cache_read_tokens", 0)

	log := models.UsageLog{
		APIKeyID:           &apiKeyID,
		ProviderID:         &providerID,
		Model:              fullModelID,
		RequestTokens:      totalInputTokens,
		ResponseTokens:     totalOutputTokens,
		TotalTokens:        totalInputTokens + totalOutputTokens,
		CacheWrite5MTokens: cacheWrite5m,
		CacheWrite1HTokens: cacheWrite1h,
		CacheReadTokens:    cacheReadTokens,
		LatencyMs:          time.Since(start).Milliseconds(),
		StartedAt:          &start,
		CompletedAt:        &completedAt,
		UserID:             &userID,
	}
	if dbModel != nil && (totalInputTokens > 0 || totalOutputTokens > 0) {
		log.Cost = calculateCost(dbModel, totalInputTokens, totalOutputTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
	}
	e.usageService.Log(log)
}

// extractDeltaContent parses SSE chunk data to extract incremental text content
// and appends it to the accumulator. Works for both OpenAI and Anthropic SSE formats.
func extractDeltaContent(chunk string, providerType string, acc *strings.Builder) {
	if acc == nil {
		return
	}
	// Only process SSE data lines
	if !strings.Contains(chunk, "data: ") {
		return
	}

	// If it looks like a message_delta or done marker, skip
	if strings.Contains(chunk, "[DONE]") || strings.Contains(chunk, "message_stop") ||
		strings.Contains(chunk, "content_block_stop") || strings.Contains(chunk, "message_delta") {
		return
	}

	// Try to find and extract text content from JSON inside "data: ..."
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			continue
		}

		// OpenAI delta format: choices[0].delta.content
		if choices, ok := data["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if content, ok := delta["content"].(string); ok && content != "" {
						acc.WriteString(content)
					}
				}
			}
			continue
		}

		// Anthropic delta format: type=content_block_delta, delta.type=text_delta, delta.text
		if dataType, ok := data["type"].(string); ok && dataType == "content_block_delta" {
			if delta, ok := data["delta"].(map[string]interface{}); ok {
				if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
					if text, ok := delta["text"].(string); ok && text != "" {
						acc.WriteString(text)
					}
				}
			}
		}
	}
}
