package proxy

// extractCacheTokens reads cache-related token counters from a usage map,
// checking every supported provider field name for each of the three cache
// categories (cacheWrite5m, cacheWrite1h, cacheRead). Values are summed
// so multiple provider fields that map to the same category are accumulated.
func extractCacheTokens(usage map[string]interface{}) (cacheWrite5m, cacheWrite1h, cacheRead int64) {
	if usage == nil {
		return 0, 0, 0
	}

	// Anthropic: cache_creation_input_tokens → cacheWrite5m
	if v := numberToInt64(usage["cache_creation_input_tokens"]); v > 0 {
		cacheWrite5m += v
	}
	// Anthropic: cache_creation_extended_input_tokens → cacheWrite1h
	if v := numberToInt64(usage["cache_creation_extended_input_tokens"]); v > 0 {
		cacheWrite1h += v
	}
	// Anthropic: cache_read_input_tokens → cacheRead
	if v := numberToInt64(usage["cache_read_input_tokens"]); v > 0 {
		cacheRead += v
	}

	// Gemini: cached_content_token_count → cacheRead
	if v := numberToInt64(usage["cached_content_token_count"]); v > 0 {
		cacheRead += v
	}

	// OpenAI: prompt_tokens_details.cached_tokens → cacheRead
	if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
		if v := numberToInt64(details["cached_tokens"]); v > 0 {
			cacheRead += v
		}
	}

	return
}

// extractAndStoreCacheTokens extracts cache tokens from a usage map and
// stores them into the streaming state map so that stream processing
// can read them back later via type assertions.
func extractAndStoreCacheTokens(usage map[string]interface{}, state map[string]interface{}) {
	if usage == nil || state == nil {
		return
	}
	cw5, cw1h, cr := extractCacheTokens(usage)
	if cw5 > 0 {
		state["cache_write_5m_tokens"] = cw5
	}
	if cw1h > 0 {
		state["cache_write_1h_tokens"] = cw1h
	}
	if cr > 0 {
		state["cache_read_tokens"] = cr
	}
}
