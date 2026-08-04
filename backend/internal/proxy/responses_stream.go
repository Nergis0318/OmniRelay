package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

// handleResponsesStream translates an upstream chat-completions SSE stream
// into OpenAI Responses API SSE events.
func (e *Engine) handleResponsesStream(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, userID int64, providerType string, inputTokens int64) {
	start := time.Now()
	state := make(map[string]interface{})
	responseID := randomID("resp_")

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

	emit := func(ev map[string]interface{}) {
		b, _ := json.Marshal(ev)
		sw.Write([]byte("data: " + string(b) + "\n\n"))
	}

	emit(map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"model":  fullModelID,
			"output": []interface{}{},
		},
	})

	type item struct {
		kind      string // "message" | "function_call"
		id        string
		callID    string
		name      string
		toolIndex int
		text      strings.Builder
		args      strings.Builder
	}

	var current *item
	var outputItems []interface{}
	var outputTextAccum strings.Builder

	closeItem := func() {
		if current == nil {
			return
		}
		idx := len(outputItems)
		switch current.kind {
		case "message":
			text := current.text.String()
			itemObj := map[string]interface{}{
				"id": current.id, "type": "message", "status": "completed", "role": "assistant",
				"content": []map[string]interface{}{{"type": "output_text", "text": text, "annotations": []interface{}{}}},
			}
			emit(map[string]interface{}{"type": "response.output_text.done", "item_id": current.id, "output_index": idx, "content_index": 0, "text": text})
			emit(map[string]interface{}{"type": "response.content_part.done", "item_id": current.id, "output_index": idx, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": text, "annotations": []interface{}{}}})
			emit(map[string]interface{}{"type": "response.output_item.done", "output_index": idx, "item": itemObj})
			outputItems = append(outputItems, itemObj)
		case "function_call":
			args := current.args.String()
			itemObj := map[string]interface{}{
				"id": current.id, "type": "function_call", "call_id": current.callID,
				"name": current.name, "arguments": args,
			}
			emit(map[string]interface{}{"type": "response.function_call_arguments.done", "item_id": current.id, "output_index": idx, "arguments": args})
			emit(map[string]interface{}{"type": "response.output_item.done", "output_index": idx, "item": itemObj})
			outputItems = append(outputItems, itemObj)
		}
		current = nil
	}

	openMessage := func() {
		itemID := randomID("msg_")
		current = &item{kind: "message", id: itemID}
		idx := len(outputItems)
		emit(map[string]interface{}{"type": "response.output_item.added", "output_index": idx, "item": map[string]interface{}{
			"id": itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []interface{}{},
		}})
		emit(map[string]interface{}{"type": "response.content_part.added", "item_id": itemID, "output_index": idx, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}}})
	}

	openFunctionCall := func(callID, name string, index int) {
		itemID := randomID("fc_")
		current = &item{kind: "function_call", id: itemID, callID: callID, name: name, toolIndex: index}
		idx := len(outputItems)
		emit(map[string]interface{}{"type": "response.output_item.added", "output_index": idx, "item": map[string]interface{}{
			"id": itemID, "type": "function_call", "call_id": callID, "name": name, "arguments": "",
		}})
	}

	finishReason := ""
	var totalInputTokens, totalOutputTokens int64
	var pending []byte
	for {
		if n > 0 {
			chunk := buf[:n]
			transformed, inTok, outTok, _ := adapter.ParseStreamChunk(chunk, state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}

			toParse := chunk
			if len(transformed) > 0 {
				toParse = transformed
			}
			pending = append(pending, toParse...)
			for {
				nl := bytes.IndexByte(pending, '\n')
				if nl < 0 {
					break
				}
				line := pending[:nl]
				pending = pending[nl+1:]
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				if !bytes.HasPrefix(line, []byte("data: ")) {
					continue
				}
				payload := line[len("data: "):]
				if string(payload) == "[DONE]" {
					continue
				}
				var chunkJSON map[string]interface{}
				if err := json.Unmarshal(payload, &chunkJSON); err != nil {
					continue
				}
				choices, _ := chunkJSON["choices"].([]interface{})
				for _, rawChoice := range choices {
					choice, _ := rawChoice.(map[string]interface{})
					delta, _ := choice["delta"].(map[string]interface{})

					if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
						for _, rawTC := range toolCalls {
							tc, _ := rawTC.(map[string]interface{})
							tcIndex := int(numberToInt64(tc["index"]))
							fn, _ := tc["function"].(map[string]interface{})
							tcName, _ := fn["name"].(string)
							tcArgs, _ := fn["arguments"].(string)

							if current == nil || current.kind != "function_call" || current.toolIndex != tcIndex {
								closeItem()
								tcID, _ := tc["id"].(string)
								openFunctionCall(tcID, tcName, tcIndex)
							}
							if tcArgs != "" {
								current.args.WriteString(tcArgs)
								emit(map[string]interface{}{"type": "response.function_call_arguments.delta", "item_id": current.id, "output_index": len(outputItems), "delta": tcArgs})
							}
						}
						continue
					}

					if content, ok := delta["content"].(string); ok && content != "" {
						if current == nil || current.kind != "message" {
							closeItem()
							openMessage()
						}
						current.text.WriteString(content)
						outputTextAccum.WriteString(content)
						emit(map[string]interface{}{"type": "response.output_text.delta", "item_id": current.id, "output_index": len(outputItems), "content_index": 0, "delta": content})
					}

					if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
						finishReason = fr
					}
				}
			}
		}
		if err != nil {
			break
		}
		n, err = resp.Body.Read(buf)
	}

	closeItem()

	if totalOutputTokens == 0 && outputTextAccum.Len() > 0 {
		totalOutputTokens = countTextTokens(outputTextAccum.String(), fullModelID)
	}
	if inputTokens > 0 {
		totalInputTokens = inputTokens
	}

	cacheWrite5m, _ := state["cache_write_5m_tokens"].(int64)
	cacheWrite1h, _ := state["cache_write_1h_tokens"].(int64)
	cacheReadTokens, _ := state["cache_read_tokens"].(int64)

	status := "completed"
	eventType := "response.completed"
	if finishReason == "length" {
		status = "incomplete"
		eventType = "response.incomplete"
	}

	emit(map[string]interface{}{
		"type": eventType,
		"response": map[string]interface{}{
			"id":          responseID,
			"object":      "response",
			"created_at":  time.Now().Unix(),
			"status":      status,
			"model":       fullModelID,
			"output":      outputItems,
			"output_text": outputTextAccum.String(),
			"usage": map[string]interface{}{
				"input_tokens":  totalInputTokens,
				"output_tokens": totalOutputTokens,
				"total_tokens":  totalInputTokens + totalOutputTokens,
				"input_tokens_details": map[string]interface{}{
					"cached_tokens": cacheReadTokens,
				},
				"output_tokens_details": map[string]interface{}{
					"reasoning_tokens": int64(0),
				},
			},
		},
	})

	sw.Write([]byte("data: [DONE]\n\n"))

	latencyMs := time.Since(start).Milliseconds()
	completedAt := time.Now()
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
