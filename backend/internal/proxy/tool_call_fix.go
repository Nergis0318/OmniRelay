package proxy

import (
	"encoding/json"
	"strings"
)

func extractValidToolNames(body map[string]interface{}) map[string]bool {
	if body == nil {
		return nil
	}
	rawTools, ok := body["tools"]
	if !ok {
		return nil
	}
	tools, ok := rawTools.([]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]bool)
	for _, raw := range tools {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if fn, ok := m["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				out[name] = true
			}
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) >= 2 {
			first := strings.TrimSpace(lines[0])
			if strings.HasPrefix(first, "```") {
				lines = lines[1:]
			}
			last := strings.TrimSpace(lines[len(lines)-1])
			if last == "```" {
				lines = lines[:len(lines)-1]
			}
			s = strings.Join(lines, "\n")
		} else {
			s = strings.TrimPrefix(s, "```")
			s = strings.TrimSuffix(s, "```")
		}
		s = strings.TrimSpace(s)
		if strings.HasPrefix(strings.ToLower(s), "json") {
			s = strings.TrimSpace(s[4:])
		}
	}
	return strings.TrimSpace(s)
}

func normalizeArguments(raw interface{}) string {
	if raw == nil {
		return "{}"
	}
	if str, ok := raw.(string); ok {
		str = strings.TrimSpace(str)
		if str == "" {
			return "{}"
		}
		var js json.RawMessage
		if err := json.Unmarshal([]byte(str), &js); err == nil {
			return str
		}
		b, _ := json.Marshal(str)
		return string(b)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func buildToolCallEntry(name string, argsRaw interface{}, id string) map[string]interface{} {
	if id == "" {
		id = randomID("call_")
	}
	return map[string]interface{}{
		"id":   id,
		"type": "function",
		"function": map[string]interface{}{
			"name":      name,
			"arguments": normalizeArguments(argsRaw),
		},
	}
}

func tryParseSingleToolCall(obj map[string]interface{}, validTools map[string]bool) (map[string]interface{}, bool) {
	var name string
	var argsRaw interface{}
	var id string

	if tcID, ok := obj["id"].(string); ok {
		id = tcID
	}
	if fn, ok := obj["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			name = n
		}
		if a, ok := fn["arguments"]; ok {
			argsRaw = a
		} else if a, ok := fn["parameters"]; ok {
			argsRaw = a
		}
	}
	if name == "" {
		if n, ok := obj["name"].(string); ok {
			name = n
		}
	}
	if name == "" {
		if n, ok := obj["tool"].(string); ok {
			name = n
		}
	}
	if argsRaw == nil {
		if a, ok := obj["arguments"]; ok {
			argsRaw = a
		} else if a, ok := obj["parameters"]; ok {
			argsRaw = a
		} else if a, ok := obj["input"]; ok {
			argsRaw = a
		} else if a, ok := obj["args"]; ok {
			argsRaw = a
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	if validTools != nil && !validTools[name] {
		return nil, false
	}
	if argsRaw == nil {
		argsRaw = map[string]interface{}{}
	}
	return buildToolCallEntry(name, argsRaw, id), true
}

func parseContentAsToolCalls(content string, validTools map[string]bool) ([]map[string]interface{}, bool) {
	cleaned := stripMarkdownFences(content)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil, false
	}

	// Quick reject: must look like JSON
	trimmed := strings.TrimSpace(cleaned)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		// Try to extract JSON substring
		extracted := extractJSONSubstring(cleaned)
		if extracted == "" {
			return nil, false
		}
		cleaned = extracted
		trimmed = strings.TrimSpace(cleaned)
	}

	var asInterface interface{}
	if err := json.Unmarshal([]byte(cleaned), &asInterface); err != nil {
		// Try substring extraction fallback
		extracted := extractJSONSubstring(cleaned)
		if extracted == "" || extracted == cleaned {
			return nil, false
		}
		if err := json.Unmarshal([]byte(extracted), &asInterface); err != nil {
			return nil, false
		}
		cleaned = extracted
	}

	switch v := asInterface.(type) {
	case []interface{}:
		var out []map[string]interface{}
		for _, elem := range v {
			obj, ok := elem.(map[string]interface{})
			if !ok {
				return nil, false
			}
			tc, ok := tryParseSingleToolCall(obj, validTools)
			if !ok {
				return nil, false
			}
			out = append(out, tc)
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	case map[string]interface{}:
		if rawTCs, ok := v["tool_calls"]; ok {
			if arr, ok := rawTCs.([]interface{}); ok {
				var out []map[string]interface{}
				for _, elem := range arr {
					obj, ok := elem.(map[string]interface{})
					if !ok {
						continue
					}
					tc, ok := tryParseSingleToolCall(obj, validTools)
					if !ok {
						continue
					}
					out = append(out, tc)
				}
				if len(out) > 0 {
					return out, true
				}
			}
		}
		if tc, ok := tryParseSingleToolCall(v, validTools); ok {
			return []map[string]interface{}{tc}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func extractJSONSubstring(s string) string {
	s = strings.TrimSpace(s)
	// Find first '{' or '['
	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")
	start := -1
	isArray := false
	if startObj >= 0 && startArr >= 0 {
		if startArr < startObj {
			start = startArr
			isArray = true
		} else {
			start = startObj
		}
	} else if startObj >= 0 {
		start = startObj
	} else if startArr >= 0 {
		start = startArr
		isArray = true
	} else {
		return ""
	}
	if isArray {
		end := strings.LastIndex(s, "]")
		if end > start {
			candidate := strings.TrimSpace(s[start : end+1])
			var js interface{}
			if err := json.Unmarshal([]byte(candidate), &js); err == nil {
				return candidate
			}
		}
	} else {
		end := strings.LastIndex(s, "}")
		if end > start {
			candidate := strings.TrimSpace(s[start : end+1])
			var js interface{}
			if err := json.Unmarshal([]byte(candidate), &js); err == nil {
				return candidate
			}
			// Try to balance braces by scanning
			depth := 0
			inStr := false
			esc := false
			for i := start; i < len(s); i++ {
				c := s[i]
				if esc {
					esc = false
					continue
				}
				if c == '\\' && inStr {
					esc = true
					continue
				}
				if c == '"' {
					inStr = !inStr
					continue
				}
				if inStr {
					continue
				}
				if c == '{' {
					depth++
				} else if c == '}' {
					depth--
					if depth == 0 {
						candidate := strings.TrimSpace(s[start : i+1])
						var js2 interface{}
						if err := json.Unmarshal([]byte(candidate), &js2); err == nil {
							return candidate
						}
						break
					}
				}
			}
		}
	}
	return ""
}

func tryFixOpenAIChatResponse(resp map[string]interface{}, reqBody map[string]interface{}) bool {
	choicesRaw, ok := resp["choices"]
	if !ok {
		return false
	}
	choices, ok := choicesRaw.([]interface{})
	if !ok || len(choices) == 0 {
		return false
	}
	validTools := extractValidToolNames(reqBody)
	fixed := false
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		msg, ok := choice["message"].(map[string]interface{})
		if !ok {
			continue
		}
		if tcs, ok := msg["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
			continue
		}
		// Also check if tool_calls exists as []map
		if tcs, ok := msg["tool_calls"].([]map[string]interface{}); ok && len(tcs) > 0 {
			continue
		}
		contentVal, hasContent := msg["content"]
		if !hasContent {
			continue
		}
		contentStr, isStr := contentVal.(string)
		if !isStr {
			if contentVal == nil {
				continue
			}
			// content could be non-string (e.g. array) - skip
			continue
		}
		if strings.TrimSpace(contentStr) == "" {
			continue
		}
		toolCalls, ok := parseContentAsToolCalls(contentStr, validTools)
		if !ok {
			continue
		}
		var converted []interface{}
		for _, tc := range toolCalls {
			converted = append(converted, tc)
		}
		msg["tool_calls"] = converted
		msg["content"] = nil
		choice["finish_reason"] = "tool_calls"
		fixed = true
	}
	return fixed
}

func tryFixStreamAccumulatedContent(accum string, validTools map[string]bool) ([]map[string]interface{}, bool) {
	accum = strings.TrimSpace(accum)
	if accum == "" {
		return nil, false
	}
	return parseContentAsToolCalls(accum, validTools)
}

func shallowCopyBody(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(body))
	for k, v := range body {
		cp[k] = v
	}
	return cp
}

func buildToolCallStreamChunks(toolCalls []map[string]interface{}) []string {
	var chunks []string
	for i, tc := range toolCalls {
		fn, _ := tc["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)
		id, _ := tc["id"].(string)
		// First delta: id + name
		first := map[string]interface{}{
			"id":     "chatcmpl_toolfix",
			"object": "chat.completion.chunk",
			"choices": []map[string]interface{}{{
				"index": 0,
				"delta": map[string]interface{}{
					"role": "assistant",
					"tool_calls": []map[string]interface{}{{
						"index": i,
						"id":    id,
						"type":  "function",
						"function": map[string]interface{}{
							"name": name,
						},
					}},
				},
				"finish_reason": nil,
			}},
		}
		if b, err := json.Marshal(first); err == nil {
			chunks = append(chunks, "data: "+string(b)+"\n\n")
		}
		// Second delta: arguments (may need chunking for large payloads)
		if args != "" && args != "{}" {
			for _, part := range chunkString(args, 3000) {
				second := map[string]interface{}{
					"id":     "chatcmpl_toolfix",
					"object": "chat.completion.chunk",
					"choices": []map[string]interface{}{{
						"index": 0,
						"delta": map[string]interface{}{
							"tool_calls": []map[string]interface{}{{
								"index": i,
								"function": map[string]interface{}{
									"arguments": part,
								},
							}},
						},
						"finish_reason": nil,
					}},
				}
				if b, err := json.Marshal(second); err == nil {
					chunks = append(chunks, "data: "+string(b)+"\n\n")
				}
			}
		}
	}
	// Final chunk: finish_reason tool_calls
	done := map[string]interface{}{
		"id":     "chatcmpl_toolfix",
		"object": "chat.completion.chunk",
		"choices": []map[string]interface{}{{
			"index":         0,
			"delta":         map[string]interface{}{},
			"finish_reason": "tool_calls",
		}},
	}
	if b, err := json.Marshal(done); err == nil {
		chunks = append(chunks, "data: "+string(b)+"\n\n")
	}
	return chunks
}

func chunkString(s string, size int) []string {
	if len(s) <= size {
		return []string{s}
	}
	var out []string
	for len(s) > 0 {
		if len(s) <= size {
			out = append(out, s)
			break
		}
		out = append(out, s[:size])
		s = s[size:]
	}
	return out
}
