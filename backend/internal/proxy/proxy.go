package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"omnirelay/internal/models"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (e *Engine) HandleChatCompletions(c *gin.Context) {
	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	fullModelID, ok := body["model"].(string)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model field is required"})
		return
	}

	dbModel, err := e.resolveModel(fullModelID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown model: %s", fullModelID)})
		return
	}

	provider, err := e.providerService.GetByID(dbModel.ProviderID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider not found or inactive"})
		return
	}

	adapter := e.getAdapter(provider.ProviderType)
	if adapter == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unsupported provider type"})
		return
	}

	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt provider key"})
		return
	}

	if dbModel.ContextWindow > 0 {
		body["_context_window"] = dbModel.ContextWindow
	}

	endpoint, adaptedBody, err := adapter.BuildChatRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isStream := false
	if stream, ok := body["stream"].(bool); ok {
		isStream = stream
	}

	adaptedJSON, _ := json.Marshal(adaptedBody)

	if provider.ProviderType == "gemini" && isStream {
		endpoint = strings.Replace(endpoint, ":generateContent", ":streamGenerateContent", 1)
		if strings.Contains(endpoint, "?") {
			endpoint += "&alt=sse"
		} else {
			endpoint += "?alt=sse"
		}
	}

	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	copyForwardableRequestHeaders(c, req)
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	if !isSuccessStatus(resp.StatusCode) {
		respBody, _ := io.ReadAll(resp.Body)
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
			UserID:       &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	if isStream {
		e.handleStreamResponse(c, resp, adapter, apiKeyID, provider.ID, fullModelID, dbModel, userID)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: "failed to read upstream response",
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read upstream response"})
		return
	}

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse, err := adapter.ParseChatResponse(upstreamResponse)
	if err != nil {
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
		latencyMs := time.Since(startTime).Milliseconds()
		completedAt := time.Now()

		e.usageService.Log(models.UsageLog{
			APIKeyID:           &apiKeyID,
			ProviderID:         &provider.ID,
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

func (e *Engine) HandleListModels(c *gin.Context) {
	userID := c.GetInt64("user_id")
	modelList, err := e.modelService.List("", userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list models"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "model path parameter is required"})
		return
	}

	userID := c.GetInt64("user_id")
	dbModel, err := e.resolveModel(fullModelID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("unknown model: %s", fullModelID)})
		return
	}

	c.JSON(http.StatusOK, dbModel.ToPublicModel())
}

func (e *Engine) HandleMessages(c *gin.Context) {
	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}

	isStream := false
	if stream, ok := body["stream"].(bool); ok {
		isStream = stream
	}

	fullModelID, ok := body["model"].(string)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model field is required"})
		return
	}

	dbModel, err := e.resolveModel(fullModelID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown model: %s", fullModelID)})
		return
	}

	provider, err := e.providerService.GetByID(dbModel.ProviderID, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider not found or inactive"})
		return
	}

	adapter := e.getAdapter(provider.ProviderType)
	if adapter == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unsupported provider type"})
		return
	}

	if isStream && provider.ProviderType != "anthropic" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "streaming /v1/messages is currently supported only for Anthropic providers"})
		return
	}

	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt provider key"})
		return
	}

	endpoint, adaptedBody, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	adaptedJSON, _ := json.Marshal(adaptedBody)
	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	copyForwardableRequestHeaders(c, req)
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	if !isSuccessStatus(resp.StatusCode) {
		respBody, _ := io.ReadAll(resp.Body)
		latencyMs := time.Since(startTime).Milliseconds()
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
			LatencyMs:    latencyMs,
			UserID:       &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	if isStream {
		e.handleRawStreamResponse(c, resp, apiKeyID, provider.ID, fullModelID, startTime, userID)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:   &apiKeyID,
			ProviderID: &provider.ID,
			Model:      fullModelID,
			LatencyMs:  latencyMs,
			UserID:     &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse, err := adapter.ParseMessagesResponse(upstreamResponse)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:   &apiKeyID,
			ProviderID: &provider.ID,
			Model:      fullModelID,
			LatencyMs:  latencyMs,
			UserID:     &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse["model"] = fullModelID

	e.usageService.Log(models.UsageLog{
		APIKeyID:   &apiKeyID,
		ProviderID: &provider.ID,
		Model:      fullModelID,
		LatencyMs:  latencyMs,
		UserID:     &userID,
	})

	c.JSON(http.StatusOK, finalResponse)
}

func (e *Engine) HandlePathRouted(c *gin.Context) {
	providerKey := c.Param("provider_key")
	endpoint := c.Param("endpoint")
	apiPrefix := routeAPIPrefix(c.Request.URL.Path)

	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	provider, err := e.providerService.GetByKey(providerKey, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown provider: %s", providerKey)})
		return
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var body map[string]interface{}
	hasRequestBody := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead
	contentType := c.GetHeader("Content-Type")

	if hasRequestBody && len(bodyBytes) > 0 && isJSONContentType(contentType) {
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			c.Data(http.StatusBadRequest, "application/json", bodyBytes)
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

	isChatCompletions := endpoint == "/chat/completions" && c.Request.Method == http.MethodPost
	isMessages := endpoint == "/messages" && c.Request.Method == http.MethodPost

	if isChatCompletions && adapter != nil && dbModel != nil {
		e.handlePathRoutedChat(c, provider, dbModel, adapter, body, fullModelID, apiKeyID, userID)
		return
	}

	if isMessages && adapter != nil && dbModel != nil {
		e.handlePathRoutedMessages(c, provider, dbModel, adapter, body, fullModelID, apiKeyID, userID)
		return
	}

	e.handlePathRoutedProxy(c, provider, dbModel, body, bodyBytes, fullModelID, apiKeyID, endpoint, apiPrefix, hasRequestBody, contentType, userID)
}

func (e *Engine) handlePathRoutedChat(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID int64, userID int64) {
	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt provider key"})
		return
	}

	if dbModel.ContextWindow > 0 {
		body["_context_window"] = dbModel.ContextWindow
	}

	endpoint, adaptedBody, err := adapter.BuildChatRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	isStream := false
	if stream, ok := body["stream"].(bool); ok {
		isStream = stream
	}

	adaptedJSON, _ := json.Marshal(adaptedBody)

	if provider.ProviderType == "gemini" && isStream {
		endpoint = strings.Replace(endpoint, ":generateContent", ":streamGenerateContent", 1)
		if strings.Contains(endpoint, "?") {
			endpoint += "&alt=sse"
		} else {
			endpoint += "?alt=sse"
		}
	}

	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &userID,
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	copyForwardableRequestHeaders(c, req)
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	if !isSuccessStatus(resp.StatusCode) {
		respBody, _ := io.ReadAll(resp.Body)
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
			UserID:       &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	if isStream {
		e.handleStreamResponse(c, resp, adapter, apiKeyID, provider.ID, fullModelID, dbModel, userID)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: "failed to read upstream response",
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read upstream response"})
		return
	}

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse, err := adapter.ParseChatResponse(upstreamResponse)
	if err != nil {
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
		latencyMs := time.Since(startTime).Milliseconds()
		completedAt := time.Now()

		e.usageService.Log(models.UsageLog{
			APIKeyID:           &apiKeyID,
			ProviderID:         &provider.ID,
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

func (e *Engine) handlePathRoutedMessages(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID int64, userID int64) {
	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt provider key"})
		return
	}

	isStream := false
	if stream, ok := body["stream"].(bool); ok {
		isStream = stream
	}

	if isStream && provider.ProviderType != "anthropic" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "streaming /messages is currently supported only for Anthropic providers"})
		return
	}

	endpoint, adaptedBody, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	adaptedJSON, _ := json.Marshal(adaptedBody)
	upstreamURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	copyForwardableRequestHeaders(c, req)
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	if !isSuccessStatus(resp.StatusCode) {
		respBody, _ := io.ReadAll(resp.Body)
		latencyMs := time.Since(startTime).Milliseconds()
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
			LatencyMs:    latencyMs,
			UserID:       &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	if isStream {
		e.handleRawStreamResponse(c, resp, apiKeyID, provider.ID, fullModelID, startTime, userID)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:   &apiKeyID,
			ProviderID: &provider.ID,
			Model:      fullModelID,
			LatencyMs:  latencyMs,
			UserID:     &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse, err := adapter.ParseMessagesResponse(upstreamResponse)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:   &apiKeyID,
			ProviderID: &provider.ID,
			Model:      fullModelID,
			LatencyMs:  latencyMs,
			UserID:     &userID,
		})
		c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
		return
	}

	finalResponse["model"] = fullModelID

	logUsageWithTokens := func(requestTokens, responseTokens int64) {
		completedAt := time.Now()
		cacheWrite5m, cacheWrite1h, cacheReadTokens := extractCacheTokens(finalResponse["usage"].(map[string]interface{}))
		cost := calculateCost(dbModel, requestTokens, responseTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
		e.usageService.Log(models.UsageLog{
			APIKeyID:           &apiKeyID,
			ProviderID:         &provider.ID,
			Model:              fullModelID,
			RequestTokens:      requestTokens,
			ResponseTokens:     responseTokens,
			TotalTokens:        requestTokens + responseTokens,
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

	if usage, ok := finalResponse["usage"].(map[string]interface{}); ok {
		inputTokens := numberToInt64(usage["input_tokens"])
		outputTokens := numberToInt64(usage["output_tokens"])
		if inputTokens > 0 || outputTokens > 0 {
			logUsageWithTokens(inputTokens, outputTokens)
		} else {
			inputTokens = numberToInt64(usage["prompt_tokens"])
			outputTokens = numberToInt64(usage["completion_tokens"])
			if inputTokens > 0 || outputTokens > 0 {
				logUsageWithTokens(inputTokens, outputTokens)
			} else {
				e.usageService.Log(models.UsageLog{
					APIKeyID:   &apiKeyID,
					ProviderID: &provider.ID,
					Model:      fullModelID,
					LatencyMs:  latencyMs,
					UserID:     &userID,
				})
			}
		}
	} else {
		e.usageService.Log(models.UsageLog{
			APIKeyID:   &apiKeyID,
			ProviderID: &provider.ID,
			Model:      fullModelID,
			LatencyMs:  latencyMs,
			UserID:     &userID,
		})
	}

	c.JSON(http.StatusOK, finalResponse)
}

func (e *Engine) handlePathRoutedProxy(c *gin.Context, provider *models.Provider, dbModel *models.Model, body map[string]interface{}, bodyBytes []byte, fullModelID string, apiKeyID int64, endpoint string, apiPrefix string, hasRequestBody bool, contentType string, userID int64) {
	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt provider key"})
		return
	}

	var reqBody io.Reader
	if hasRequestBody {
		if len(body) > 0 && isJSONContentType(contentType) {
			adaptedJSON, _ := json.Marshal(body)
			reqBody = bytes.NewReader(adaptedJSON)
		} else if len(bodyBytes) > 0 {
			reqBody = bytes.NewReader(bodyBytes)
		}
	}

	upstreamBaseURL := routedBaseURL(provider.ProviderType, apiPrefix, provider.APiBaseURL)
	upstreamEndpoint := routedEndpoint(apiPrefix, upstreamBaseURL, endpoint)
	upstreamURL := appendRawQuery(joinUpstreamURL(upstreamBaseURL, upstreamEndpoint), c.Request.URL.RawQuery)

	req, err := http.NewRequest(c.Request.Method, upstreamURL, reqBody)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &userID,
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return
	}

	if hasRequestBody {
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	copyForwardableRequestHeaders(c, req)
	setProviderAuthHeaders(req, provider.ProviderType, apiKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: err.Error(),
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	bodyStream, _ := body["stream"].(bool)
	responseContentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if isSuccessStatus(resp.StatusCode) && (bodyStream || strings.Contains(responseContentType, "text/event-stream") || strings.Contains(responseContentType, "x-ndjson")) {
		e.handleRawStreamResponse(c, resp, apiKeyID, provider.ID, fullModelID, startTime, userID)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	if !isSuccessStatus(resp.StatusCode) {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
			LatencyMs:    latencyMs,
			UserID:       &userID,
		})
	} else if dbModel != nil {
		var respJSON map[string]interface{}
		if json.Unmarshal(respBody, &respJSON) == nil {
			requestTokens, responseTokens, totalTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens := extractUsageFromRawResponse(provider.ProviderType, respJSON)
			if requestTokens > 0 || responseTokens > 0 {
				completedAt := time.Now()
				cost := calculateCost(dbModel, requestTokens, responseTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
				e.usageService.Log(models.UsageLog{
					APIKeyID:           &apiKeyID,
					ProviderID:         &provider.ID,
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
			} else {
				e.usageService.Log(models.UsageLog{
					APIKeyID:   &apiKeyID,
					ProviderID: &provider.ID,
					Model:      fullModelID,
					LatencyMs:  latencyMs,
					UserID:     &userID,
				})
			}
		} else {
			e.usageService.Log(models.UsageLog{
				APIKeyID:   &apiKeyID,
				ProviderID: &provider.ID,
				Model:      fullModelID,
				LatencyMs:  latencyMs,
				UserID:     &userID,
			})
		}
	} else {
		e.usageService.Log(models.UsageLog{
			APIKeyID:   &apiKeyID,
			ProviderID: &provider.ID,
			Model:      fullModelID,
			LatencyMs:  latencyMs,
			UserID:     &userID,
		})
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
	return
}
