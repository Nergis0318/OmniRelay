package proxy

import (
	"encoding/json"

	"omnirelay/internal/models"
)

func calculateCost(m *models.Model, inputTokens, outputTokens, cacheWrite5mTokens, cacheWrite1hTokens, cacheReadTokens int64) float64 {
	inputCost := (float64(inputTokens) / 1000000.0) * m.InputPricePer1MTok
	outputCost := (float64(outputTokens) / 1000000.0) * m.OutputPricePer1MTok
	cacheWrite5mCost := (float64(cacheWrite5mTokens) / 1000000.0) * m.CacheWrite5mPricePer1MTok
	cacheWrite1hCost := (float64(cacheWrite1hTokens) / 1000000.0) * m.CacheWrite1hPricePer1MTok
	cacheReadCost := (float64(cacheReadTokens) / 1000000.0) * m.CacheReadPricePer1MTok
	return inputCost + outputCost + cacheWrite5mCost + cacheWrite1hCost + cacheReadCost
}

func numberToInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func extractUsageFromRawResponse(providerType string, body map[string]interface{}) (requestTokens, responseTokens, totalTokens, cacheWrite5m, cacheWrite1h, cacheRead int64) {
	switch providerType {
	case "anthropic":
		if usage, ok := body["usage"].(map[string]interface{}); ok {
			requestTokens = numberToInt64(usage["input_tokens"])
			responseTokens = numberToInt64(usage["output_tokens"])
			cacheWrite5m, cacheWrite1h, cacheRead = extractCacheTokens(usage)
		}
	case "gemini":
		if usage, ok := body["usageMetadata"].(map[string]interface{}); ok {
			requestTokens = numberToInt64(usage["promptTokenCount"])
			responseTokens = numberToInt64(usage["candidatesTokenCount"])
			totalTokens = numberToInt64(usage["totalTokenCount"])
			_, _, cacheRead = extractCacheTokens(usage)
		}
	default:
		if usage, ok := body["usage"].(map[string]interface{}); ok {
			requestTokens = numberToInt64(usage["prompt_tokens"])
			responseTokens = numberToInt64(usage["completion_tokens"])
			totalTokens = numberToInt64(usage["total_tokens"])
			cacheWrite5m, cacheWrite1h, cacheRead = extractCacheTokens(usage)
		}
	}
	if totalTokens == 0 && (requestTokens > 0 || responseTokens > 0) {
		totalTokens = requestTokens + responseTokens
	}
	return
}
