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

	dbModel, err := e.resolveModel(fullModelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown model: %s", fullModelID)})
		return
	}

	provider, err := e.providerService.GetByID(dbModel.ProviderID)
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

	upstreamURL := strings.TrimRight(provider.APiBaseURL, "/") + endpoint

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if provider.ProviderType == "gemini" {
		req.Header.Set("x-goog-api-key", apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if provider.ProviderType == "anthropic" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	if provider.ProviderType == "gemini" && isStream {
		upstreamURL = strings.Replace(upstreamURL, ":generateContent", ":streamGenerateContent?alt=sse", 1)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:    &apiKeyID,
			ProviderID:  &provider.ID,
			Model:       fullModelID,
			IsError:     true,
			ErrorMessage: err.Error(),
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	if isStream {
		e.handleStreamResponse(c, resp, adapter, apiKeyID, provider.ID, fullModelID, dbModel)
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
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read upstream response"})
		return
	}

	if resp.StatusCode != http.StatusOK {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
		})
		c.Data(resp.StatusCode, "application/json", respBody)
		return
	}

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		c.Data(resp.StatusCode, "application/json", respBody)
		return
	}

	finalResponse, err := adapter.ParseChatResponse(upstreamResponse)
	if err != nil {
		c.Data(resp.StatusCode, "application/json", respBody)
		return
	}

	finalResponse["model"] = fullModelID

	if usage, ok := finalResponse["usage"].(map[string]interface{}); ok {
		requestTokens, _ := usage["prompt_tokens"].(int64)
		responseTokens, _ := usage["completion_tokens"].(int64)
		totalTokens, _ := usage["total_tokens"].(int64)
		cacheWriteTokens, cacheHitTokens := extractCacheTokens(usage)

		cost := calculateCost(dbModel, int64(requestTokens), int64(responseTokens), cacheWriteTokens, cacheHitTokens)
		latencyMs := time.Since(startTime).Milliseconds()

		e.usageService.Log(models.UsageLog{
			APIKeyID:       &apiKeyID,
			ProviderID:     &provider.ID,
			Model:          fullModelID,
			RequestTokens:  int64(requestTokens),
			ResponseTokens: int64(responseTokens),
			TotalTokens:    int64(totalTokens),
			LatencyMs:      latencyMs,
			Cost:           cost,
		})
	}

	c.JSON(http.StatusOK, finalResponse)
}

func (e *Engine) handleStreamResponse(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model) {
	c.Status(http.StatusOK)
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

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]

			transformed, _ := adapter.ParseStreamChunk(chunk)
			if transformed != nil {
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

	c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()

	latencyMs := time.Since(start).Milliseconds()

	cost := calculateCost(dbModel, totalInputTokens, totalOutputTokens, 0, 0)

	e.usageService.Log(models.UsageLog{
		APIKeyID:       &apiKeyID,
		ProviderID:     &providerID,
		Model:          fullModelID,
		RequestTokens:  totalInputTokens,
		ResponseTokens: totalOutputTokens,
		TotalTokens:    totalInputTokens + totalOutputTokens,
		LatencyMs:      latencyMs,
		Cost:           cost,
	})
}

func (e *Engine) HandleListModels(c *gin.Context) {
	modelList, err := e.modelService.List("")
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

func (e *Engine) HandleMessages(c *gin.Context) {
	apiKeyID := c.GetInt64("api_key_id")

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

	dbModel, err := e.resolveModel(fullModelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown model: %s", fullModelID)})
		return
	}

	provider, err := e.providerService.GetByID(dbModel.ProviderID)
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

	endpoint, adaptedBody, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	adaptedJSON, _ := json.Marshal(adaptedBody)
	upstreamURL := strings.TrimRight(provider.APiBaseURL, "/") + endpoint

	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(adaptedJSON))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if provider.ProviderType == "gemini" {
		req.Header.Set("x-goog-api-key", apiKey)
	} else if provider.ProviderType == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

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
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
			LatencyMs:    latencyMs,
		})
		c.Data(resp.StatusCode, "application/json", respBody)
		return
	}

	var upstreamResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &upstreamResponse); err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:  &apiKeyID,
			ProviderID: &provider.ID,
			Model:     fullModelID,
			LatencyMs: latencyMs,
		})
		c.Data(resp.StatusCode, "application/json", respBody)
		return
	}

	finalResponse, err := adapter.ParseMessagesResponse(upstreamResponse)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:  &apiKeyID,
			ProviderID: &provider.ID,
			Model:     fullModelID,
			LatencyMs: latencyMs,
		})
		c.Data(resp.StatusCode, "application/json", respBody)
		return
	}

	finalResponse["model"] = fullModelID

	e.usageService.Log(models.UsageLog{
		APIKeyID:  &apiKeyID,
		ProviderID: &provider.ID,
		Model:     fullModelID,
		LatencyMs: latencyMs,
	})

	c.JSON(http.StatusOK, finalResponse)
}

func (e *Engine) HandlePathRouted(c *gin.Context) {
	providerKey := c.Param("provider_key")
	endpoint := c.Param("endpoint")

	provider, err := e.providerService.GetByKey(providerKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown provider: %s", providerKey)})
		return
	}

	apiKeyID := c.GetInt64("api_key_id")

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var body map[string]interface{}
	var adaptedJSON []byte
	isGet := c.Request.Method == "GET"

	if !isGet && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			c.Data(http.StatusBadRequest, "application/json", bodyBytes)
			return
		}
	}

	if body == nil {
		body = make(map[string]interface{})
	}

	if modelID, ok := body["model"].(string); ok {
		for i := 0; i < len(modelID); i++ {
			if modelID[i] == '/' {
				body["model"] = modelID[i+1:]
				break
			}
		}
	}

	fullModelID := providerKey + "/"
	if m, ok := body["model"].(string); ok {
		fullModelID += m
	} else {
		fullModelID += "unknown"
	}

	apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decrypt provider key"})
		return
	}

	adaptedJSON, _ = json.Marshal(body)
	upstreamURL := strings.TrimRight(provider.APiBaseURL, "/") + endpoint

	var reqBody io.Reader
	if !isGet {
		reqBody = bytes.NewReader(adaptedJSON)
	}
	req, err := http.NewRequest(c.Request.Method, upstreamURL, reqBody)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:  &apiKeyID,
			ProviderID: &provider.ID,
		Model:     fullModelID,
		IsError:   true,
		ErrorMessage: err.Error(),
	})
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upstream request"})
	return
}

if !isGet {
	req.Header.Set("Content-Type", "application/json")
}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if provider.ProviderType == "gemini" {
		req.Header.Set("x-goog-api-key", apiKey)
	}
	if provider.ProviderType == "anthropic" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:  &apiKeyID,
			ProviderID: &provider.ID,
			Model:     fullModelID,
			IsError:   true,
			ErrorMessage: err.Error(),
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(startTime).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		e.usageService.Log(models.UsageLog{
			APIKeyID:  &apiKeyID,
			ProviderID: &provider.ID,
			Model:     fullModelID,
			IsError:   true,
			ErrorMessage: fmt.Sprintf("upstream returned %d", resp.StatusCode),
			LatencyMs: latencyMs,
		})
	} else {
		e.usageService.Log(models.UsageLog{
			APIKeyID:  &apiKeyID,
			ProviderID: &provider.ID,
			Model:     fullModelID,
			LatencyMs: latencyMs,
		})
	}

	for k, values := range resp.Header {
		for _, v := range values {
			c.Header(k, v)
		}
	}
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
}

func calculateCost(m *models.Model, inputTokens int64, outputTokens int64, cacheWriteTokens int64, cacheHitTokens int64) float64 {
	inputCost := (float64(inputTokens) / 1000000.0) * m.InputPricePer1MTok
	outputCost := (float64(outputTokens) / 1000000.0) * m.OutputPricePer1MTok
	cacheWrite5mCost := (float64(cacheWriteTokens) / 1000000.0) * m.CacheWrite5mPricePer1MTok
	cacheReadCost := (float64(cacheHitTokens) / 1000000.0) * m.CacheReadPricePer1MTok
	return inputCost + outputCost + cacheWrite5mCost + cacheReadCost
}

func extractCacheTokens(usage map[string]interface{}) (int64, int64) {
	var cacheWrite, cacheHit int64
	if cw, ok := usage["cache_creation_input_tokens"].(float64); ok {
		cacheWrite = int64(cw)
	}
	if cw, ok := usage["cache_read_input_tokens"].(float64); ok {
		cacheHit = int64(cw)
	}
	if cw, ok := usage["cache_creation_input_tokens"].(int64); ok {
		cacheWrite = cw
	}
	if cw, ok := usage["cache_read_input_tokens"].(int64); ok {
		cacheHit = cw
	}
	return cacheWrite, cacheHit
}
