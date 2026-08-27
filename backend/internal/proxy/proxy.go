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

	dbModel, provider, adapter, _, ok := e.resolveDispatch(c, fullModelID, userID, apiresponse.FormatOpenAI, apiFormatOpenAI)
	if !ok {
		return
	}

	e.executeChat(c, provider, dbModel, adapter, body, fullModelID, apiKeyID, userID)
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

	dbModel, provider, adapter, apiKey, ok := e.resolveDispatch(c, fullModelID, userID, apiresponse.FormatAnthropic, apiFormatAnthropic)
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

	if e.authService != nil {
		if allowed, _ := e.authService.AllowedProviderSet(userID); allowed != nil {
			filtered := modelList[:0]
			for _, m := range modelList {
				if allowed[m.ProviderID] {
					filtered = append(filtered, m)
				}
			}
			modelList = filtered
		}
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

	if e.authService != nil {
		if allowed, _ := e.authService.CanAccessProvider(userID, provider.ID); !allowed {
			apiresponse.AbortForbidden(c, errFmt, "access to this provider is not permitted for your account")
			return
		}
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

	// Route to real model if model has source_provider_key
	if dbModel != nil && dbModel.SourceProviderKey != "" {
		if sourceProvider, serr := e.providerService.GetByKey(dbModel.SourceProviderKey, userID); serr == nil {
			if e.authService != nil {
				if allowed, _ := e.authService.CanAccessProvider(userID, sourceProvider.ID); !allowed {
					apiresponse.AbortForbidden(c, errFmt, "access to this provider is not permitted for your account")
					return
				}
			}
			provider = sourceProvider
		}
	}

	u := usageContext{
		apiKeyID:    apiKeyID,
		providerID:  provider.ID,
		userID:      userID,
		fullModelID: fullModelID,
	}

	isChatCompletions := endpoint == "/chat/completions" && c.Request.Method == http.MethodPost
	isMessages := endpoint == "/messages" && c.Request.Method == http.MethodPost

	format := apiFormatOpenAI
	if isMessages || strings.HasPrefix(apiPrefix, "v1beta") {
		format = apiFormatAnthropic
	}
	provider = effectiveProvider(provider, format)
	adapter := e.getAdapter(provider.ProviderType)

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
		e.executeChat(c, provider, dbModel, adapter, body, fullModelID, u.apiKeyID, u.userID)
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
func (e *Engine) resolveDispatch(c *gin.Context, fullModelID string, userID int64, errFmt apiresponse.Format, format apiFormat) (*models.Model, *models.Provider, Adapter, string, bool) {
	dbModel, err := e.resolveModel(fullModelID, userID)
	if err != nil {
		apiresponse.AbortNotFound(c, errFmt, fmt.Sprintf("The model '%s' does not exist", fullModelID), "model")
		return nil, nil, nil, "", false
	}

	var provider *models.Provider
	if dbModel.SourceProviderKey != "" {
		provider, err = e.providerService.GetByKey(dbModel.SourceProviderKey, userID)
	} else {
		provider, err = e.providerService.GetByID(dbModel.ProviderID, userID)
	}
	if err != nil {
		apiresponse.AbortInvalidRequest(c, errFmt, "provider not found or inactive", "model")
		return nil, nil, nil, "", false
	}

	if e.authService != nil {
		if allowed, _ := e.authService.CanAccessProvider(userID, provider.ID); !allowed {
			apiresponse.AbortForbidden(c, errFmt, "access to this provider is not permitted for your account")
			return nil, nil, nil, "", false
		}
	}

	provider = effectiveProvider(provider, format)

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

func (e *Engine) executeMessages(c *gin.Context, body map[string]interface{}, fullModelID string, dbModel *models.Model, provider *models.Provider, adapter Adapter, apiKey string, u usageContext) {
	endpoint, adaptedBody, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatAnthropic, err.Error(), "")
		return
	}

	// Count input tokens locally before sending to upstream
	localInputTokens := countInputTokens(body, fullModelID)

	isStream, _ := body["stream"].(bool)
	if isStream && isOpenAICompat(provider.ProviderType) {
		if _, ok := adaptedBody["stream_options"]; !ok {
			adaptedBody["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}
	endpoint = applyGeminiStreamingURL(provider.ProviderType, endpoint, isStream)
	modelURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	resp, startTime, ok := e.proxyJSONRequest(c, u, provider.ProviderType, apiKey, modelURL, adaptedBody, true)
	if !ok {
		return
	}
	defer resp.Body.Close()

	if isStream {
		e.handleMessagesStreamResponse(c, resp, adapter, u.apiKeyID, u.providerID, u.fullModelID, dbModel, startTime, u.userID, provider.ProviderType, localInputTokens)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	if isEmptyResponseBody(respBody) {
		e.logUpstreamError(u, "the model returned an empty response", latencyMs)
		apiresponse.AbortBadGateway(c, apiresponse.FormatFromContext(c), "the model returned an empty response")
		return
	}

	var modelResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &modelResponse); err != nil {
		e.logLatencyOnly(u, latencyMs)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse, err := adapter.ParseMessagesResponse(modelResponse)
	if err != nil {
		e.logLatencyOnly(u, latencyMs)
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	if errMsg := extractErrorContent(finalResponse); errMsg != "" {
		e.logUpstreamError(u, errMsg, latencyMs)
		abortErrorContent(c, errMsg)
		return
	}

	finalResponse["model"] = fullModelID

	// Count output tokens locally
	localOutputTokens := countOutputTokens(finalResponse, provider.ProviderType, fullModelID)

	// Extract upstream token data (for cache tokens and fallback)
	usage, hasUsage := finalResponse["usage"].(map[string]interface{})

	var upstreamInput, upstreamOutput int64
	if hasUsage {
		upstreamInput = numberToInt64(usage["input_tokens"])
		if upstreamInput == 0 {
			upstreamInput = numberToInt64(usage["prompt_tokens"])
		}
		upstreamOutput = numberToInt64(usage["output_tokens"])
		if upstreamOutput == 0 {
			upstreamOutput = numberToInt64(usage["completion_tokens"])
		}
	}

	inputTokens := upstreamInput
	if inputTokens == 0 {
		inputTokens = localInputTokens
	}
	outputTokens := upstreamOutput
	if outputTokens == 0 {
		outputTokens = localOutputTokens
	}

	if inputTokens == 0 && outputTokens == 0 {
		// Even if input/output tokens are 0, check for cache tokens
		if hasUsage {
			cacheWrite5m, cacheWrite1h, cacheReadTokens := extractCacheTokens(usage)
			if cacheWrite5m > 0 || cacheWrite1h > 0 || cacheReadTokens > 0 {
				completedAt := time.Now()
				cost := calculateCost(dbModel, 0, 0, cacheWrite5m, cacheWrite1h, cacheReadTokens)
				e.logTokenUsage(u, tokenUsage{
					cacheWrite5m: cacheWrite5m,
					cacheWrite1h: cacheWrite1h,
					cacheRead:    cacheReadTokens,
					cost:         cost,
					startedAt:    &startTime,
					completedAt:  &completedAt,
					latencyMs:    latencyMs,
				})
				c.JSON(http.StatusOK, finalResponse)
				return
			}
		}
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

func (e *Engine) handlePathRoutedProxy(c *gin.Context, provider *models.Provider, dbModel *models.Model, body map[string]interface{}, bodyBytes []byte, fullModelID string, endpoint string, apiPrefix string, hasRequestBody bool, contentType string, u usageContext) {
	errFmt := apiresponse.FormatFromContext(c)

	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		apiresponse.AbortInternal(c, errFmt, "failed to decrypt provider key")
		return
	}

	// Count input tokens locally from the request body
	localInputTokens := countInputTokens(body, fullModelID)

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

	modelBaseURL := routedBaseURL(provider.ProviderType, apiPrefix, provider.APiBaseURL)
	modelEndpoint := routedEndpoint(apiPrefix, modelBaseURL, endpoint)
	modelURL := appendRawQuery(joinUpstreamURL(modelBaseURL, modelEndpoint), c.Request.URL.RawQuery)

	req, err := buildUpstreamRequest(c, c.Request.Method, modelURL, reqBodyBytes, provider.ProviderType, apiKey)
	if err != nil {
		e.logUpstreamError(u, err.Error(), 0)
		apiresponse.AbortInternal(c, errFmt, "failed to create the model request")
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
		apiresponse.AbortBadGateway(c, errFmt, fmt.Sprintf("the model request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	bodyStream, _ := body["stream"].(bool)
	responseContentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if isSuccessStatus(resp.StatusCode) && (bodyStream || strings.Contains(responseContentType, "text/event-stream") || strings.Contains(responseContentType, "x-ndjson")) {
		if adapter := e.getAdapter(provider.ProviderType); adapter != nil {
			e.handleStreamResponse(c, resp, adapter, u.apiKeyID, u.providerID, u.fullModelID, dbModel, u.userID, provider.ProviderType, localInputTokens)
		} else {
			e.handleRawStreamResponse(c, resp, u.apiKeyID, u.providerID, u.fullModelID, startTime, u.userID)
		}
		return
	}

	if !isSuccessStatus(resp.StatusCode) {
		latencyMs := time.Since(startTime).Milliseconds()
		e.logUpstreamError(u, fmt.Sprintf("the model returned %d", resp.StatusCode), latencyMs)
		writeUpstreamErrorBody(c, resp, provider.ProviderType)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	if isEmptyResponseBody(respBody) {
		e.logUpstreamError(u, "the model returned an empty response", latencyMs)
		apiresponse.AbortBadGateway(c, errFmt, "the model returned an empty response")
		return
	}

	var respJSON map[string]interface{}
	if json.Unmarshal(respBody, &respJSON) == nil {
		if errMsg := extractErrorContent(respJSON); errMsg != "" {
			e.logUpstreamError(u, errMsg, latencyMs)
			abortErrorContent(c, errMsg)
			return
		}

		// Count output tokens locally
		localOutputTokens := countOutputTokens(respJSON, provider.ProviderType, fullModelID)

		// Extract upstream token data (for cache tokens and fallback)
		upstreamReq, upstreamResp, _, cacheWrite5m, cacheWrite1h, cacheReadTokens := extractUsageFromRawResponse(provider.ProviderType, respJSON)

		requestTokens := upstreamReq
		if requestTokens == 0 {
			requestTokens = localInputTokens
		}
		responseTokens := upstreamResp
		if responseTokens == 0 {
			responseTokens = localOutputTokens
		}
		totalTokens := requestTokens + responseTokens

		if requestTokens > 0 || responseTokens > 0 || cacheWrite5m > 0 || cacheWrite1h > 0 || cacheReadTokens > 0 {
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

func isOpenAICompat(providerType string) bool {
	switch providerType {
	case "openai", "lmstudio", "ollama":
		return true
	}
	return false
}
