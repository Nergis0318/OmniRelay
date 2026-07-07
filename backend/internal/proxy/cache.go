package proxy

// cacheField is a single field extraction rule used by extractCacheTokens.
type cacheField struct {
	key   string
	target string // "cacheWrite5m", "cacheWrite1h", or "cacheRead"
}

var cacheFieldRules = []cacheField{
	// Anthropic: standard cache write (5-minute TTL)
	{key: "cache_creation_input_tokens", target: "cacheWrite5m"},
	// Anthropic: extended cache write (1-hour TTL)
	{key: "cache_creation_extended_input_tokens", target: "cacheWrite1h"},
	// Anthropic: cache read
	{key: "cache_read_input_tokens", target: "cacheRead"},
}

// extractCacheTokens reads cache-related token counters from a usage map,
// checking every supported provider field name for each of the three cache
// categories (cacheWrite5m, cacheWrite1h, cacheRead). Values are additive
// so multiple provider fields that map to the same category are summed.
//
//nolint:unparam // signature matches callers across all providers
func extractCacheTokens(usage map[string]interface{}) (cacheWrite5m, cacheWrite1h, cacheRead int64) {
	if usage == nil {
		return 0, 0, 0
	}

	// 1. Rule-driven fields (Anthropic naming)
	for _, rule := range cacheFieldRules {
		v := numberToInt64(usage[rule.key])
		if v <= 0 {
			continue
		}
		switch rule.target {
		case "cacheWrite5m":
			cacheWrite5m += v
		case "cacheWrite1h":
			cacheWrite1h += v
		case "cacheRead":
			cacheRead += v
		}
	}

	// 2. Gemini: cached_content_token_count → cacheRead
	if v := numberToInt64(usage["cached_content_token_count"]); v > 0 {
		cacheRead += v
	}

	// 3. OpenAI: prompt_tokens_details.cached_tokens → cacheRead
	if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
		if v := numberToInt64(details["cached_tokens"]); v > 0 {
			cacheRead += v
		}
	}

	return
}

// extractAndStoreCacheTokens extracts cache tokens from a usage map and
// stores them into the streaming state map so that stream.go can read them
// back later via int64State(state, "cache_write_5m_tokens", 0) etc.
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
