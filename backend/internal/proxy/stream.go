package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

const streamKeepAliveInterval = 15 * time.Second

// streamWriter serializes SSE writes so the keepalive pinger and the main
// reader can share c.Writer / flusher without races. Write always flushes.
type streamWriter struct {
	w       io.Writer
	flusher http.Flusher
	mu      sync.Mutex
	// atLineStart is true when the last write ended with a newline, so a
	// keepalive comment can be emitted without splitting a data: payload.
	atLineStart bool
}

func newStreamWriter(w io.Writer, flusher http.Flusher) *streamWriter {
	return &streamWriter{w: w, flusher: flusher, atLineStart: true}
}

func (s *streamWriter) Write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.w.Write(p)
	if len(p) > 0 {
		s.atLineStart = p[len(p)-1] == '\n'
	}
	s.flusher.Flush()
}

// WriteKeepAlive writes an SSE comment line, but only when the stream is at a
// line boundary. Upstream chunks can split a data: payload mid-JSON; writing a
// comment then would corrupt the line (e.g. `data: {...: keepalive`) and break
// the client's JSON parser. When mid-line the ping is skipped for this tick.
func (s *streamWriter) WriteKeepAlive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.atLineStart {
		return
	}
	_, _ = s.w.Write([]byte(": keepalive\n\n"))
	s.flusher.Flush()
}

// startKeepAlive launches a goroutine that writes SSE comment lines to keep
// intermediaries (Cloudflare, Caddy) from timing out during upstream silence.
// done is closed when the stream ends. Returns nothing — the goroutine exits
// on its own when done is closed. Both the pinger and the main thread use the
// same *streamWriter to avoid concurrent access to c.Writer.
func startKeepAlive(sw *streamWriter, done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(streamKeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sw.WriteKeepAlive()
			}
		}
	}()
}

// writeStreamUpstreamError converts an upstream error detected mid-stream
// into a standard error response. Interruptions are temporary → 503
// retryable (overloaded_error/server_error); other upstream errors keep the
// legacy 502 api_error shape.
func writeStreamUpstreamError(c *gin.Context, errMsg string) {
	errFmt := apiresponse.FormatFromContext(c)
	requestID := c.GetString("request_id")
	// Some interruption error events omit the message field; treat those as
	// interruptions anyway and fall back to the canonical wording.
	if errMsg == "" {
		errMsg = "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."
	}
	// Interruptions and "Empty message" are temporary upstream failures →
	// 503 retryable (server_error / overloaded_error).
	if isInterruptionText(errMsg) || isUpstreamErrorContent(errMsg) {
		errType := "overloaded_error"
		if errFmt != apiresponse.FormatAnthropic {
			errType = "server_error"
		}
		c.Status(http.StatusServiceUnavailable)
		c.Header("Content-Type", "application/json")
		c.Writer.Write(reformatError(upstreamError{ErrType: errType, Message: errMsg, Code: "upstream_error"}, errFmt, requestID))
		return
	}
	c.Status(http.StatusBadGateway)
	c.Header("Content-Type", "application/json")
	c.Writer.Write(reformatError(upstreamError{ErrType: "api_error", Message: errMsg}, errFmt, requestID))
}

func (e *Engine) handleStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, userID int64, providerType string, inputTokens int64) {
	start := time.Now()
	buf := make([]byte, 4096)
	state := make(map[string]interface{})

	n, err := resp.Body.Read(buf)
	for n == 0 && err == nil {
		n, err = resp.Body.Read(buf)
	}
	if n == 0 {
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, "the model returned an empty response", time.Since(start).Milliseconds())
		apiresponse.AbortBadGateway(c, apiresponse.FormatFromContext(c), "the model returned an empty response")
		return
	}

	c.Status(http.StatusOK)
	copyResponseHeaders(c, resp.Header)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	done := make(chan struct{})
	defer close(done)
	sw := newStreamWriter(c.Writer, flusher)
	startKeepAlive(sw, done)

	var totalInputTokens, totalOutputTokens int64
	var outputTextAccum strings.Builder
	var head bytes.Buffer
	headOpen := true
	sentDone := false

	flushHead := func() {
		if headOpen {
			sw.Write(head.Bytes())
			head.Reset()
			headOpen = false
		}
	}

	for {
		if n > 0 {
			chunk := buf[:n]
			chunkStr := string(chunk)
			if strings.Contains(chunkStr, "data: [DONE]") {
				sentDone = true
			}

			extractDeltaContent(chunkStr, providerType, &outputTextAccum)

			transformed, inTok, outTok, _ := adapter.ParseStreamChunk(chunk, state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}
			// Upstream error (e.g. Anthropic interruption) detected mid-stream:
			// abort before anything content-bearing reaches the client.
			if _, errSet := state["upstream_error"]; errSet {
				break
			}

			var out []byte
			if transformed != nil {
				if strings.Contains(string(transformed), "data: [DONE]") {
					sentDone = true
				}
				out = transformed
			} else {
				out = chunk
			}
			// Hold the pre-content head (role/empty chunks) until the first
			// content-bearing chunk so a mid-stream upstream error can still
			// become a clean 503.
			if headOpen && (strings.Contains(string(out), "content") || strings.Contains(string(out), "finish_reason") || sentDone) {
				flushHead()
			}
			if headOpen {
				head.Write(out)
			} else {
				sw.Write(out)
			}
		}
		if err != nil {
			break
		}
		n, err = resp.Body.Read(buf)
	}

	latencyMs := time.Since(start).Milliseconds()

	if errMsg, ok := state["upstream_error"].(string); ok {
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, errMsg, latencyMs)
		writeStreamUpstreamError(c, errMsg)
		return
	}
	flushHead()

	if !sentDone {
		sw.Write([]byte("data: [DONE]\n\n"))
	}

	completedAt := time.Now()

	if totalOutputTokens == 0 && outputTextAccum.Len() > 0 {
		totalOutputTokens = countTextTokens(outputTextAccum.String(), fullModelID)
	}
	if inputTokens > 0 {
		totalInputTokens = inputTokens
	}

	cacheWrite5m, _ := state["cache_write_5m_tokens"].(int64)
	cacheWrite1h, _ := state["cache_write_1h_tokens"].(int64)
	cacheReadTokens, _ := state["cache_read_tokens"].(int64)
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
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	for n == 0 && err == nil {
		n, err = resp.Body.Read(buf)
	}
	if n == 0 {
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, "the model returned an empty response", time.Since(start).Milliseconds())
		apiresponse.AbortBadGateway(c, apiresponse.FormatFromContext(c), "the model returned an empty response")
		return
	}

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

	done := make(chan struct{})
	defer close(done)
	sw := newStreamWriter(c.Writer, flusher)
	startKeepAlive(sw, done)

	for {
		if n > 0 {
			sw.Write(buf[:n])
		}
		if err != nil {
			break
		}
		n, err = resp.Body.Read(buf)
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
	state := make(map[string]interface{})
	var totalInputTokens, totalOutputTokens int64
	var outputTextAccum strings.Builder
	var streamBuf bytes.Buffer
	readBuf := make([]byte, 4096)

	for {
		n, err := resp.Body.Read(readBuf)
		if n > 0 {
			chunk := readBuf[:n]
			extractDeltaContent(string(chunk), providerType, &outputTextAccum)

			transformed, inTok, outTok, _ := adapter.ParseMessagesStreamChunk(chunk, state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}
			if transformed != nil {
				streamBuf.Write(transformed)
			}
		}
		if err != nil {
			break
		}
	}

	if errMsg, ok := state["upstream_error"].(string); ok {
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, errMsg, time.Since(start).Milliseconds())
		writeStreamUpstreamError(c, errMsg)
		return
	}

	if streamBuf.Len() == 0 {
		errMsg := "the model returned an empty response"
		errFmt := apiresponse.FormatFromContext(c)
		requestID := c.GetString("request_id")
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, errMsg, time.Since(start).Milliseconds())
		c.Status(http.StatusBadGateway)
		c.Header("Content-Type", "application/json")
		c.Writer.Write(reformatError(upstreamError{ErrType: "api_error", Message: errMsg}, errFmt, requestID))
		return
	}

	c.Status(http.StatusOK)
	copyResponseHeaders(c, resp.Header)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	done := make(chan struct{})
	defer close(done)
	sw := newStreamWriter(c.Writer, flusher)
	startKeepAlive(sw, done)

	sw.Write(streamBuf.Bytes())

	completedAt := time.Now()
	latencyMs := time.Since(start).Milliseconds()

	// Prefer locally counted output tokens over upstream values
	if totalOutputTokens == 0 && outputTextAccum.Len() > 0 {
		totalOutputTokens = countTextTokens(outputTextAccum.String(), fullModelID)
	}
	// Prefer locally counted input tokens over upstream values
	if inputTokens > 0 {
		totalInputTokens = inputTokens
	}

	cacheWrite5m, _ := state["cache_write_5m_tokens"].(int64)
	cacheWrite1h, _ := state["cache_write_1h_tokens"].(int64)
	cacheReadTokens, _ := state["cache_read_tokens"].(int64)

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
		LatencyMs:          latencyMs,
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
