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

// startSSE writes 200 + streaming headers and returns a keepalive-pinged
// writer for the response. forceEventStream overrides an upstream
// Content-Type with text/event-stream; when false an existing upstream
// Content-Type is preserved. ok is false when the ResponseWriter cannot
// flush — callers must return without writing. The caller must defer
// close(done) to stop the keepalive pinger.
func startSSE(c *gin.Context, resp *http.Response, forceEventStream bool) (sw *streamWriter, done chan struct{}, ok bool) {
	c.Status(http.StatusOK)
	copyResponseHeaders(c, resp.Header)
	if forceEventStream || resp.Header.Get("Content-Type") == "" {
		c.Header("Content-Type", "text/event-stream")
	}
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, canFlush := c.Writer.(http.Flusher)
	if !canFlush {
		return nil, nil, false
	}
	done = make(chan struct{})
	sw = newStreamWriter(c.Writer, flusher)
	startKeepAlive(sw, done)
	return sw, done, true
}

// responsesBuffer detects Responses-API format streams mid-flight ("type":"response."
// split across chunk boundaries), buffers them whole, and at end-of-stream turns
// the known "Empty message" failure text into a clean error instead of forwarding
// it as content.
type responsesBuffer struct {
	active     bool
	carry      string
	buf        bytes.Buffer
	deltaAccum strings.Builder
	finalText  string
}

// observe inspects one raw chunk. It returns true when the chunk belongs to a
// Responses-API stream and was buffered instead of being forwarded; the caller
// must then not write the chunk to the client.
func (rb *responsesBuffer) observe(chunk []byte) bool {
	chunkStr := string(chunk)
	if !rb.active && strings.Contains(rb.carry+chunkStr, `"type":"response.`) {
		rb.active = true
	}
	if len(chunkStr) > 15 {
		rb.carry = chunkStr[len(chunkStr)-15:]
	} else {
		rb.carry = chunkStr
	}
	if !rb.active {
		return false
	}
	rb.buf.Write(chunk)
	extractResponsesOutputText(chunkStr, &rb.deltaAccum, &rb.finalText)
	return true
}

// failureText returns the accumulated failure text when the buffered stream
// ends with the known upstream failure content, else "".
func (rb *responsesBuffer) failureText() string {
	if !rb.active {
		return ""
	}
	text := rb.finalText
	if text == "" {
		text = rb.deltaAccum.String()
	}
	if isUpstreamErrorContent(text) {
		return text
	}
	return ""
}

func (rb *responsesBuffer) flush(sw *streamWriter) {
	if rb.active {
		sw.Write(rb.buf.Bytes())
	}
}

// writeResponsesFailure reports a detected Responses-API stream failure:
// a clean error status when nothing was written yet, an in-stream error
// event otherwise. Always returns true so callers can `return` unconditionally.
func (e *Engine) writeResponsesFailure(c *gin.Context, sw *streamWriter, u usageContext, text string, latencyMs int64) bool {
	e.logUpstreamError(u, text, latencyMs)
	if !c.Writer.Written() {
		writeStreamUpstreamError(c, text)
		return true
	}
	errFmt := apiresponse.FormatFromContext(c)
	errType := "server_error"
	if errFmt == apiresponse.FormatAnthropic {
		errType = "overloaded_error"
	}
	sw.Write([]byte("data: " + string(reformatError(upstreamError{ErrType: errType, Message: text, Code: "upstream_error"}, errFmt, c.GetString("request_id"))) + "\n\n"))
	return true
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

	sw, done, ok := startSSE(c, resp, true)
	if !ok {
		return
	}
	defer close(done)

	var totalInputTokens, totalOutputTokens int64
	var outputTextAccum strings.Builder
	var head bytes.Buffer
	headOpen := true
	sentDone := false
	var firstTokenAt *time.Time
	var ttftMs *int64
	var rb responsesBuffer

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

			prevLen := outputTextAccum.Len()
			extractDeltaContent(chunkStr, providerType, &outputTextAccum)
			if firstTokenAt == nil && outputTextAccum.Len() > prevLen {
				now := time.Now()
				firstTokenAt = &now
			}

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

			if rb.observe(chunk) {
				n, err = resp.Body.Read(buf)
				continue
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

	if text := rb.failureText(); text != "" {
		e.writeResponsesFailure(c, sw, usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, text, latencyMs)
		return
	}

	flushHead()
	rb.flush(sw)

	if !sentDone {
		sw.Write([]byte("data: [DONE]\n\n"))
	}

	completedAt := time.Now()
	if firstTokenAt != nil {
		v := firstTokenAt.Sub(start).Milliseconds()
		ttftMs = &v
	}

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
		TTFTMs:             ttftMs,
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

	sw, done, ok := startSSE(c, resp, false)
	if !ok {
		return
	}
	defer close(done)

	var rb responsesBuffer

	for {
		if n > 0 {
			chunk := buf[:n]
			if rb.observe(chunk) {
				n, err = resp.Body.Read(buf)
				continue
			}
			sw.Write(chunk)
		}
		if err != nil {
			break
		}
		n, err = resp.Body.Read(buf)
	}

	if text := rb.failureText(); text != "" {
		e.writeResponsesFailure(c, sw, usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, text, time.Since(start).Milliseconds())
		return
	}
	rb.flush(sw)

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
	var firstTokenAt *time.Time

	for {
		n, err := resp.Body.Read(readBuf)
		if n > 0 {
			chunk := readBuf[:n]
			prevLen := outputTextAccum.Len()
			extractDeltaContent(string(chunk), providerType, &outputTextAccum)
			if firstTokenAt == nil && outputTextAccum.Len() > prevLen {
				now := time.Now()
				firstTokenAt = &now
			}

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

	sw, done, ok := startSSE(c, resp, true)
	if !ok {
		return
	}
	defer close(done)

	sw.Write(streamBuf.Bytes())

	completedAt := time.Now()
	latencyMs := time.Since(start).Milliseconds()
	var ttftMs *int64
	if firstTokenAt != nil {
		v := firstTokenAt.Sub(start).Milliseconds()
		ttftMs = &v
	}

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
		TTFTMs:             ttftMs,
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

// extractResponsesOutputText accumulates output text from Responses-API SSE
// events. deltaAccum grows with output_text.delta deltas; finalText holds the
// authoritative full text once output_text.done or response.completed arrives
// (deltas alone are not enough — some upstreams skip the delta events).
func extractResponsesOutputText(chunk string, deltaAccum *strings.Builder, finalText *string) {
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev["type"] {
		case "response.output_text.delta":
			if d, ok := ev["delta"].(string); ok {
				deltaAccum.WriteString(d)
			}
		case "response.output_text.done":
			if t, ok := ev["text"].(string); ok {
				*finalText = t
			}
		case "response.completed":
			if resp, ok := ev["response"].(map[string]interface{}); ok {
				if t, ok := resp["output_text"].(string); ok {
					*finalText = t
				}
			}
		}
	}
}
