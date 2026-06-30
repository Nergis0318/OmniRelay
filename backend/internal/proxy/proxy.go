package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (e *Engine) HandleChatCompletions(c *gin.Context) {
	ensureRequestID(c)
	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	body, ok := readJSONBody(c)
	if !ok {
		return
	}
	if param, err := apiresponse.ValidateChatCompletionBody(body); err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, err.Error(), param)
		return
	}

	fullModelID := body["model"].(string)

	dbModel, provider, adapter, apiKey, ok := e.resolveDispatch(c, fullModelID, userID, apiresponse.FormatOpenAI)
	if !ok {
		return
	}

	e.executeChat(c, body, fullModelID, dbModel, provider, adapter, apiKey, usageContext{
		apiKeyID:    apiKeyID,
		providerID:  provider.ID,
		userID:      userID,
		fullModelID: fullModelID,
	})
}

func (e *Engine) HandleMessages(c *gin.Context) {
	ensureRequestID(c)
	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	body, ok := readJSONBody(c)
	if !ok {
		return
	}
	if param, err := apiresponse.ValidateMessagesBody(body); err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatAnthropic, err.Error(), param)
		return
	}

	fullModelID := body["model"].(string)

	dbModel, provider, adapter, apiKey, ok := e.resolveDispatch(c, fullModelID, userID, apiresponse.FormatAnthropic)
	if !ok {
		return
	}

	e.executeMessages(c, body, fullModelID, dbModel, provider, adapter, apiKey, usageContext{
		apiKeyID:    apiKeyID,
		providerID:  provider.ID,
		userID:      userID,
		fullModelID: fullModelID,
	})
}

func (e *Engine) HandleListModels(c *gin.Context) {
	userID := c.GetInt64("user_id")
	modelList, err := e.modelService.List("", userID)
	if err != nil {
		apiresponse.AbortInternal(c, apiresponse.FormatOpenAI, "failed to list models")
		return
	}

	var data []models.PublicModel
	for _, m := range modelList {
		data = append(data, m.ToPublicModel())
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
	})
}

func (e *Engine) HandleGetModel(c *gin.Context) {
	fullModelID := strings.TrimPrefix(c.Param("model"), "/")
	if fullModelID == "" {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, "model path parameter is required", "model")
		return
	}

	userID := c.GetInt64("user_id")
	dbModel, err := e.resolveModel(fullModelID, userID)
	if err != nil {
		apiresponse.AbortNotFound(c, apiresponse.FormatOpenAI, fmt.Sprintf("The model '%s' does not exist", fullModelID), "model")
		return
	}

	c.JSON(http.StatusOK, dbModel.ToPublicModel())
}

func (e *Engine) HandlePathRouted(c *gin.Context) {
	ensureRequestID(c)
	providerKey := c.Param("provider_key")
	endpoint := c.Param("endpoint")
	apiPrefix := routeAPIPrefix(c.Request.URL.Path)

	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	errFmt := apiresponse.FormatFromContext(c)

	provider, err := e.providerService.GetByKey(providerKey, userID)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, errFmt, fmt.Sprintf("unknown provider: %s", providerKey), "provider")
		return
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, errFmt, "failed to read request body", "")
		return
	}

	var body map[string]interface{}
	hasRequestBody := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead
	contentType := c.GetHeader("Content-Type")

	if hasRequestBody && len(bodyBytes) > 0 && isJSONContentType(contentType) {
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			apiresponse.AbortInvalidRequest(c, errFmt, "invalid JSON in request body", "")
			return
		}
	}

	if body == nil {
		body = make(map[string]interface{})
	}

	if modelID, ok := body["model"].(string); ok {
		body["model"] = stripProviderPrefix(modelID)
	}

	fullModelID := providerKey + "/"
	if m, ok := body["model"].(string); ok {
		fullModelID += m
	} else {
		fullModelID += "unknown"
	}

	dbModel, _ := e.resolveModel(fullModelID, userID)
	adapter := e.getAdapter(provider.ProviderType)

	u := usageContext{
		apiKeyID:    apiKeyID,
		providerID:  provider.ID,
		userID:      userID,
		fullModelID: fullModelID,
	}

	isChatCompletions := endpoint == "/chat/completions" && c.Request.Method == http.MethodPost
	isMessages := endpoint == "/messages" && c.Request.Method == http.MethodPost

	if isChatCompletions && hasRequestBody && isJSONContentType(contentType) {
		if param, err := apiresponse.ValidateChatCompletionBody(body); err != nil {
			apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, err.Error(), param)
			return
		}
	}
	if isMessages && hasRequestBody && isJSONContentType(contentType) {
		if param, err := apiresponse.ValidateMessagesBody(body); err != nil {
			apiresponse.AbortInvalidRequest(c, apiresponse.FormatAnthropic, err.Error(), param)
			return
		}
	}

	if isChatCompletions && adapter != nil && dbModel != nil {
		apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
		if err != nil {
			apiresponse.AbortInternal(c, apiresponse.FormatOpenAI, "failed to decrypt provider key")
			return
		}
		e.executeChat(c, body, fullModelID, dbModel, provider, adapter, apiKey, u)
		return
	}

	if isMessages && adapter != nil && dbModel != nil {
		apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
		if err != nil {
			apiresponse.AbortInternal(c, apiresponse.FormatAnthropic, "failed to decrypt provider key")
			return
		}
		e.executeMessages(c, body, fullModelID, dbModel, provider, adapter, apiKey, u)
		return
	}

	e.handlePathRoutedProxy(c, provider, dbModel, body, bodyBytes, fullModelID, endpoint, apiPrefix, hasRequestBody, contentType, u)
}

// resolveDispatch resolves dbModel/provider/adapter/apiKey for body-driven endpoints (HandleChatCompletions / HandleMessages).
// It writes the appropriate error response if anything fails.
func (e *Engine) resolveDispatch(c *gin.Context, fullModelID string, userID int64, errFmt apiresponse.Format) (*models.Model, *models.Provider, Adapter, string, bool) {
	dbModel, err := e.resolveModel(fullModelID, userID)
	if err != nil {
		apiresponse.AbortNotFound(c, errFmt, fmt.Sprintf("The model '%s' does not exist", fullModelID), "model")
		return nil, nil, nil, "", false
	}

	provider, err := e.providerService.GetByID(dbModel.ProviderID, userID)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, errFmt, "provider not found or inactive", "model")
		return nil, nil, nil, "", false
	}

	adapter := e.getAdapter(provider.ProviderType)
	if adapter == nil {
		apiresponse.AbortInternal(c, errFmt, "unsupported provider type")
		return nil, nil, nil, "", false
	}

	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		apiresponse.AbortInternal(c, errFmt, "failed to decrypt provider key")
		return nil, nil, nil, "", false
	}

	return dbModel, provider, adapter, apiKey, true
}

func readJSONBody(c *gin.Context) (map[string]interface{}, bool) {
	errFmt := apiresponse.FormatFromContext(c)
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, errFmt, "failed to read request body", "")
		return nil, false
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		apiresponse.AbortInvalidRequest(c, errFmt, "invalid JSON in request body", "")
		return nil, false
	}
	return body, true
}

// executeChat handles the OpenAI-style /chat/completions request lifecycle for both the direct and path-routed entries.
func (e *Engine) executeChat(c *gin.Context, body map[string]interface{}, fullModelID string, dbModel *models.Model, provider *models.Provider, adapter Adapter, apiKey string, u usageContext) {
	if dbModel.ContextWindow > 0 {
		body["_context_window"] = dbModel.ContextWindow
	}

	endpoint, adaptedBody, err := adapter.BuildChatRequest(body)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, err.Error(), "")
		return
	}

	isStream, _ := body["stream"].(bool)
	if isStream && isOpenAICompat(provider.ProviderType) {
		if _, ok := adaptedBody["stream_options"]; !ok {
			adaptedBody["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}
	endpoint = applyGeminiStreamingURL(provider.ProviderType, endpoint, isStream)
	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	resp, startTime, ok := e.proxyJSONRequest(c, u, provider.ProviderType, apiKey, upstreamURL, adaptedBody, true)
	if !ok {
		return
	}
	defer resp.Body.Close()

	if isStream {
		e.handleStreamResponse(c, resp, adapter, dbModel, startTime, u)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		e.logUpstreamError(u, "failed to read upstream response", time.Since(startTime).Milliseconds())
		apiresponse.AbortBadGateway(c, apiresponse.FormatOpenAI, "failed to read upstream response")
		return
	}

	latencyMs := time.Since(startTime).Milliseconds()

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		e.logLatencyOnly(u, latencyMs)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse, err := adapter.ParseChatResponse(upstreamResponse)
	if err != nil {
		e.logLatencyOnly(u, latencyMs)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse["model"] = fullModelID

	if usage, ok := finalResponse["usage"].(map[string]interface{}); ok {
		requestTokens := numberToInt64(usage["prompt_tokens"])
		responseTokens := numberToInt64(usage["completion_tokens"])
		totalTokens := numberToInt64(usage["total_tokens"])
		cacheWrite5m, cacheWrite1h, cacheReadTokens := extractCacheTokens(usage)

		cost := calculateCost(dbModel, requestTokens, responseTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
		completedAt := time.Now()

		e.logTokenUsage(u, tokenUsage{
			requestTokens:  requestTokens,
			responseTokens: responseTokens,
			totalTokens:    totalTokens,
			cacheWrite5m:   cacheWrite5m,
			cacheWrite1h:   cacheWrite1h,
			cacheRead:      cacheReadTokens,
			cost:           cost,
			startedAt:      &startTime,
			completedAt:    &completedAt,
			latencyMs:      latencyMs,
		})
	} else {
		e.logLatencyOnly(u, latencyMs)
	}

	c.JSON(http.StatusOK, finalResponse)
}

// executeMessages handles the Anthropic-style /messages request lifecycle for both the direct and path-routed entries.
func (e *Engine) executeMessages(c *gin.Context, body map[string]interface{}, fullModelID string, dbModel *models.Model, provider *models.Provider, adapter Adapter, apiKey string, u usageContext) {
	endpoint, adaptedBody, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatAnthropic, err.Error(), "")
		return
	}

	isStream, _ := body["stream"].(bool)
	if isStream && isOpenAICompat(provider.ProviderType) {
		if _, ok := adaptedBody["stream_options"]; !ok {
			adaptedBody["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}
	endpoint = applyGeminiStreamingURL(provider.ProviderType, endpoint, isStream)
	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	resp, startTime, ok := e.proxyJSONRequest(c, u, provider.ProviderType, apiKey, upstreamURL, adaptedBody, true)
	if !ok {
		return
	}
	defer resp.Body.Close()

	if isStream {
		e.handleMessagesStreamResponse(c, resp, adapter, dbModel, startTime, u)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		e.logLatencyOnly(u, latencyMs)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse, err := adapter.ParseMessagesResponse(upstreamResponse)
	if err != nil {
		e.logLatencyOnly(u, latencyMs)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse["model"] = fullModelID

	usage, hasUsage := finalResponse["usage"].(map[string]interface{})
	if !hasUsage {
		e.logLatencyOnly(u, latencyMs)
		c.JSON(http.StatusOK, finalResponse)
		return
	}

	inputTokens := numberToInt64(usage["input_tokens"])
	outputTokens := numberToInt64(usage["output_tokens"])
	if inputTokens == 0 && outputTokens == 0 {
		inputTokens = numberToInt64(usage["prompt_tokens"])
		outputTokens = numberToInt64(usage["completion_tokens"])
	}

	if inputTokens == 0 && outputTokens == 0 {
		e.logLatencyOnly(u, latencyMs)
		c.JSON(http.StatusOK, finalResponse)
		return
	}

	cacheWrite5m, cacheWrite1h, cacheReadTokens := extractCacheTokens(usage)
	cost := calculateCost(dbModel, inputTokens, outputTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
	completedAt := time.Now()

	e.logTokenUsage(u, tokenUsage{
		requestTokens:  inputTokens,
		responseTokens: outputTokens,
		cacheWrite5m:   cacheWrite5m,
		cacheWrite1h:   cacheWrite1h,
		cacheRead:      cacheReadTokens,
		cost:           cost,
		startedAt:      &startTime,
		completedAt:    &completedAt,
		latencyMs:      latencyMs,
	})

	c.JSON(http.StatusOK, finalResponse)
}

func (e *Engine) handleStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, dbModel *models.Model, start time.Time, u usageContext) {
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
	var totalInputTokens, totalOutputTokens int64
	sentDone := false
	state := make(map[string]interface{})

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if strings.Contains(string(chunk), "data: [DONE]") {
				sentDone = true
			}

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
			if err != io.EOF {
				requestID := c.GetString("request_id")
				errorPayload := map[string]interface{}{
					"error": map[string]interface{}{
						"type":       "api_error",
						"message":    "upstream stream interrupted",
						"request_id": requestID,
					},
				}
				if errorJSON, e := json.Marshal(errorPayload); e == nil {
					fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errorJSON)
					flusher.Flush()
				}
			}
			break
		}
	}

	if !sentDone {
		c.Writer.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}

	latencyMs := time.Since(start).Milliseconds()
	completedAt := time.Now()
	cacheWrite5m := int64State(state, "cache_write_5m_tokens", 0)
	cacheReadTokens := int64State(state, "cache_read_tokens", 0)
	var cost float64
	if dbModel != nil && (totalInputTokens > 0 || totalOutputTokens > 0) {
		cost = calculateCost(dbModel, totalInputTokens, totalOutputTokens, cacheWrite5m, 0, cacheReadTokens)
	}

	e.logTokenUsage(u, tokenUsage{
		requestTokens:  totalInputTokens,
		responseTokens: totalOutputTokens,
		cacheWrite5m:   cacheWrite5m,
		cacheRead:      cacheReadTokens,
		cost:           cost,
		startedAt:      &start,
		completedAt:    &completedAt,
		latencyMs:      latencyMs,
	})
}

func (e *Engine) handleRawStreamResponse(c *gin.Context, resp *http.Response, start time.Time, u usageContext) {
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
			if err != io.EOF {
				requestID := c.GetString("request_id")
				errorPayload := map[string]interface{}{
					"error": map[string]interface{}{
						"type":       "api_error",
						"message":    "upstream stream interrupted",
						"request_id": requestID,
					},
				}
				if errorJSON, e := json.Marshal(errorPayload); e == nil {
					fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errorJSON)
					flusher.Flush()
				}
			}
			break
		}
	}

	e.logLatencyOnly(u, time.Since(start).Milliseconds())
}

func (e *Engine) handleMessagesStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, dbModel *models.Model, start time.Time, u usageContext) {
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
			if err != io.EOF {
				requestID := c.GetString("request_id")
				errorPayload := map[string]interface{}{
					"error": map[string]interface{}{
						"type":       "api_error",
						"message":    "upstream stream interrupted",
						"request_id": requestID,
					},
				}
				if errorJSON, e := json.Marshal(errorPayload); e == nil {
					fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errorJSON)
					flusher.Flush()
				}
			}
			break
		}
	}

	completedAt := time.Now()
	latencyMs := time.Since(start).Milliseconds()
	cacheWrite5m := int64State(state, "cache_write_5m_tokens", 0)
	cacheReadTokens := int64State(state, "cache_read_tokens", 0)
	var cost float64
	if dbModel != nil && (totalInputTokens > 0 || totalOutputTokens > 0) {
		cost = calculateCost(dbModel, totalInputTokens, totalOutputTokens, cacheWrite5m, 0, cacheReadTokens)
	}

	e.logTokenUsage(u, tokenUsage{
		requestTokens:  totalInputTokens,
		responseTokens: totalOutputTokens,
		cacheWrite5m:   cacheWrite5m,
		cacheRead:      cacheReadTokens,
		cost:           cost,
		startedAt:      &start,
		completedAt:    &completedAt,
		latencyMs:      latencyMs,
	})
}

// handlePathRoutedProxy is the catch-all path-routed request handler used when the request is not
// a known /chat/completions or /messages call (or when no model record exists in the DB).
func (e *Engine) handlePathRoutedProxy(c *gin.Context, provider *models.Provider, dbModel *models.Model, body map[string]interface{}, bodyBytes []byte, fullModelID string, endpoint string, apiPrefix string, hasRequestBody bool, contentType string, u usageContext) {
	errFmt := apiresponse.FormatFromContext(c)

	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		apiresponse.AbortInternal(c, errFmt, "failed to decrypt provider key")
		return
	}

	var reqBodyBytes []byte
	if hasRequestBody {
		bodyStream, _ := body["stream"].(bool)
		if bodyStream && isOpenAICompat(provider.ProviderType) {
			if _, ok := body["stream_options"]; !ok {
				body["stream_options"] = map[string]interface{}{"include_usage": true}
			}
		}
		if len(body) > 0 && isJSONContentType(contentType) {
			reqBodyBytes, _ = json.Marshal(body)
		} else if len(bodyBytes) > 0 {
			reqBodyBytes = bodyBytes
		}
	}

	upstreamBaseURL := routedBaseURL(provider.ProviderType, apiPrefix, provider.APiBaseURL)
	upstreamEndpoint := routedEndpoint(apiPrefix, upstreamBaseURL, endpoint)
	upstreamURL := appendRawQuery(joinUpstreamURL(upstreamBaseURL, upstreamEndpoint), c.Request.URL.RawQuery)

	req, err := buildUpstreamRequest(c, c.Request.Method, upstreamURL, reqBodyBytes, provider.ProviderType, apiKey)
	if err != nil {
		e.logUpstreamError(u, err.Error(), 0)
		apiresponse.AbortInternal(c, errFmt, "failed to create upstream request")
		return
	}
	if hasRequestBody {
		ct := contentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}

	resp, startTime, err := e.doUpstream(req)
	if err != nil {
		e.logUpstreamError(u, err.Error(), 0)
		apiresponse.AbortBadGateway(c, errFmt, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	bodyStream, _ := body["stream"].(bool)
	responseContentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if isSuccessStatus(resp.StatusCode) && (bodyStream || strings.Contains(responseContentType, "text/event-stream") || strings.Contains(responseContentType, "x-ndjson")) {
		if adapter := e.getAdapter(provider.ProviderType); adapter != nil {
			e.handleStreamResponse(c, resp, adapter, dbModel, startTime, u)
		} else {
			e.handleRawStreamResponse(c, resp, startTime, u)
		}
		return
	}

	if !isSuccessStatus(resp.StatusCode) {
		latencyMs := time.Since(startTime).Milliseconds()
		e.logUpstreamError(u, fmt.Sprintf("upstream returned %d", resp.StatusCode), latencyMs)
		writeUpstreamErrorBody(c, resp, provider.ProviderType)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	var respJSON map[string]interface{}
	if json.Unmarshal(respBody, &respJSON) == nil {
		requestTokens, responseTokens, totalTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens := extractUsageFromRawResponse(provider.ProviderType, respJSON)
		if requestTokens > 0 || responseTokens > 0 {
			completedAt := time.Now()
			var cost float64
			if dbModel != nil {
				cost = calculateCost(dbModel, requestTokens, responseTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
			}
			e.logTokenUsage(u, tokenUsage{
				requestTokens:  requestTokens,
				responseTokens: responseTokens,
				totalTokens:    totalTokens,
				cacheWrite5m:   cacheWrite5m,
				cacheWrite1h:   cacheWrite1h,
				cacheRead:      cacheReadTokens,
				cost:           cost,
				startedAt:      &startTime,
				completedAt:    &completedAt,
				latencyMs:      latencyMs,
			})
		} else {
			e.logLatencyOnly(u, latencyMs)
		}
	} else {
		e.logLatencyOnly(u, latencyMs)
	}

	copyResponseHeaders(c, resp.Header)
	c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
}

func calculateCost(m *models.Model, inputTokens, outputTokens, cacheWrite5mTokens, cacheWrite1hTokens, cacheReadTokens int64) float64 {
	inputCost := (float64(inputTokens) / 1000000.0) * m.InputPricePer1MTok
	outputCost := (float64(outputTokens) / 1000000.0) * m.OutputPricePer1MTok
	cacheWrite5mCost := (float64(cacheWrite5mTokens) / 1000000.0) * m.CacheWrite5mPricePer1MTok
	cacheWrite1hCost := (float64(cacheWrite1hTokens) / 1000000.0) * m.CacheWrite1hPricePer1MTok
	cacheReadCost := (float64(cacheReadTokens) / 1000000.0) * m.CacheReadPricePer1MTok
	return inputCost + outputCost + cacheWrite5mCost + cacheWrite1hCost + cacheReadCost
}

func extractCacheTokens(usage map[string]interface{}) (cacheWrite5m, cacheWrite1h, cacheRead int64) {
	cacheWrite5m = numberToInt64(usage["cache_creation_input_tokens"])
	cacheRead = numberToInt64(usage["cache_read_input_tokens"])
	if cacheRead == 0 {
		cacheRead = numberToInt64(usage["cached_content_token_count"])
	}
	if cacheRead == 0 {
		if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
			cacheRead = numberToInt64(details["cached_tokens"])
		}
	}
	return
}

func isOpenAICompat(providerType string) bool {
	switch providerType {
	case "openai", "lmstudio", "ollama":
		return true
	}
	return false
}
