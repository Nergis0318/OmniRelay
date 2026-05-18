package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type GeminiAdapter struct{}

func (a *GeminiAdapter) BuildChatRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	strippedModel := stripProviderPrefix(modelID)

	geminiBody := make(map[string]interface{})

	var systemParts []map[string]interface{}
	var contents []map[string]interface{}

	messages, ok := body["messages"].([]interface{})
	if !ok {
		return "", nil, fmt.Errorf("messages field is required")
	}

	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		parts := convertOpenAIContentToGeminiParts(msg["content"])

		if role == "system" {
			for _, p := range parts {
				systemParts = append(systemParts, p)
			}
			continue
		}

		geminiRole := "user"
		if role == "assistant" {
			geminiRole = "model"
		}

		contents = append(contents, map[string]interface{}{
			"role":  geminiRole,
			"parts": parts,
		})
	}

	if len(systemParts) > 0 {
		geminiBody["system_instruction"] = map[string]interface{}{
			"parts": systemParts,
		}
	}

	geminiBody["contents"] = contents

	if temp, ok := body["temperature"]; ok {
		setGenConfig(geminiBody, "temperature", temp)
	}
	if maxTokens, ok := body["max_tokens"]; ok {
		setGenConfig(geminiBody, "maxOutputTokens", maxTokens)
	}
	if maxTokens, ok := body["max_completion_tokens"]; ok {
		setGenConfig(geminiBody, "maxOutputTokens", maxTokens)
	}
	if topP, ok := body["top_p"]; ok {
		setGenConfig(geminiBody, "topP", topP)
	}
	if n, ok := body["n"]; ok {
		setGenConfig(geminiBody, "candidateCount", n)
	}
	if stop, ok := body["stop"]; ok {
		setGenConfig(geminiBody, "stopSequences", stop)
	}
	if responseFormat, ok := body["response_format"].(map[string]interface{}); ok {
		if formatType, _ := responseFormat["type"].(string); formatType == "json_object" || formatType == "json_schema" {
			setGenConfig(geminiBody, "responseMimeType", "application/json")
		}
	}

	endpoint := "/models/" + strippedModel + ":generateContent"
	return endpoint, geminiBody, nil
}

// setGenConfig assigns a key inside body["generationConfig"], creating the map on first call.
func setGenConfig(body map[string]interface{}, key string, value interface{}) {
	gc, ok := body["generationConfig"].(map[string]interface{})
	if !ok {
		gc = make(map[string]interface{})
		body["generationConfig"] = gc
	}
	gc[key] = value
}

func convertOpenAIContentToGeminiParts(content interface{}) []map[string]interface{} {
	var parts []map[string]interface{}

	switch c := content.(type) {
	case string:
		if c != "" {
			parts = append(parts, map[string]interface{}{"text": c})
		}
	case []interface{}:
		for _, item := range c {
			part, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			partType, _ := part["type"].(string)
			switch partType {
			case "text":
				parts = append(parts, map[string]interface{}{
					"text": part["text"],
				})
			case "image_url":
				imageURL, _ := part["image_url"].(map[string]interface{})
				url, _ := imageURL["url"].(string)
				if strings.HasPrefix(url, "data:") {
					parts = append(parts, map[string]interface{}{
						"inlineData": map[string]interface{}{
							"mimeType": extractMimeType(url),
							"data":     extractBase64Data(url),
						},
					})
				} else if url != "" {
					parts = append(parts, map[string]interface{}{
						"fileData": map[string]interface{}{
							"fileUri": url,
						},
					})
				}
			}
		}
	}
	return parts
}

func extractMimeType(dataURL string) string {
	if idx := strings.Index(dataURL, ":"); idx >= 0 {
		end := strings.Index(dataURL, ";")
		if end > idx {
			return dataURL[idx+1 : end]
		}
	}
	return "image/jpeg"
}

func extractBase64Data(dataURL string) string {
	if idx := strings.Index(dataURL, ","); idx >= 0 {
		return dataURL[idx+1:]
	}
	return dataURL
}

func (a *GeminiAdapter) ParseChatResponse(body map[string]interface{}) (map[string]interface{}, error) {
	response := make(map[string]interface{})

	response["id"] = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	response["object"] = "chat.completion"
	response["created"] = time.Now().Unix()

	var choices []map[string]interface{}
	var requestTokens, responseTokens, totalTokens int64

	candidates, _ := body["candidates"].([]interface{})
	for i, rawCandidate := range candidates {
		candidate, _ := rawCandidate.(map[string]interface{})
		content, _ := candidate["content"].(map[string]interface{})

		var textContent string
		parts, _ := content["parts"].([]interface{})
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]interface{})
			if text, ok := part["text"].(string); ok {
				textContent += text
			}
		}

		finishReason := "stop"
		if fr, ok := candidate["finishReason"].(string); ok {
			switch fr {
			case "STOP":
				finishReason = "stop"
			case "MAX_TOKENS":
				finishReason = "length"
			case "SAFETY":
				finishReason = "content_filter"
			case "RECITATION":
				finishReason = "content_filter"
			default:
				finishReason = "stop"
			}
		}

		choices = append(choices, map[string]interface{}{
			"index": i,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": textContent,
			},
			"finish_reason": finishReason,
		})
	}

	response["choices"] = choices

	if usage, ok := body["usageMetadata"].(map[string]interface{}); ok {
		if pt, ok := usage["promptTokenCount"].(float64); ok {
			requestTokens = int64(pt)
		}
		if ct, ok := usage["candidatesTokenCount"].(float64); ok {
			responseTokens = int64(ct)
		}
		if tt, ok := usage["totalTokenCount"].(float64); ok {
			totalTokens = int64(tt)
		}

		response["usage"] = map[string]interface{}{
			"prompt_tokens":     requestTokens,
			"completion_tokens": responseTokens,
			"total_tokens":      totalTokens,
		}
	}

	return response, nil
}

func (a *GeminiAdapter) ParseStreamChunk(data []byte) ([]byte, int64, int64, error) {
	text := strings.TrimSpace(string(data))

	var result bytes.Buffer
	var totalInput, totalOutput int64

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		var deltaText string
		candidates, _ := event["candidates"].([]interface{})
		for _, rawCandidate := range candidates {
			candidate, _ := rawCandidate.(map[string]interface{})
			content, _ := candidate["content"].(map[string]interface{})
			parts, _ := content["parts"].([]interface{})
			for _, rawPart := range parts {
				part, _ := rawPart.(map[string]interface{})
				if t, ok := part["text"].(string); ok {
					deltaText += t
				}
			}
		}

		chunk := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"content": deltaText}},
			},
		}

		if usage, ok := event["usageMetadata"].(map[string]interface{}); ok {
			input := int64(0)
			output := int64(0)
			if v, ok := usage["promptTokenCount"].(float64); ok {
				input = int64(v)
			}
			if v, ok := usage["candidatesTokenCount"].(float64); ok {
				output = int64(v)
			}
			totalInput = input
			totalOutput = output
			chunk["usage"] = map[string]interface{}{
				"prompt_tokens":     input,
				"completion_tokens": output,
				"total_tokens":      input + output,
			}
		}

		jsonChunk, _ := json.Marshal(chunk)
		result.WriteString("data: ")
		result.Write(jsonChunk)
		result.WriteString("\n\n")
	}

	if result.Len() == 0 {
		jsonChunk, _ := json.Marshal(map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}},
			},
		})
		result.WriteString("data: ")
		result.Write(jsonChunk)
		result.WriteString("\n\n")
	}

	return result.Bytes(), totalInput, totalOutput, nil
}

func (a *GeminiAdapter) ParseMessagesStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error) {
	text := strings.TrimSpace(string(data))
	var result bytes.Buffer
	var totalInput, totalOutput int64

	writeEvent := func(event map[string]interface{}) {
		jsonEvent, _ := json.Marshal(event)
		result.WriteString("event: ")
		result.WriteString(fmt.Sprint(event["type"]))
		result.WriteString("\n")
		result.WriteString("data: ")
		result.Write(jsonEvent)
		result.WriteString("\n\n")
	}

	ensureStarted := func() {
		if !boolState(state, "started") {
			writeEvent(map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":            fmt.Sprintf("msg_%d", time.Now().UnixNano()),
					"type":          "message",
					"role":          "assistant",
					"content":       []interface{}{},
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]interface{}{
						"input_tokens": 0,
					},
				},
			})
			state["started"] = true
		}
		if !boolState(state, "content_started") {
			writeEvent(map[string]interface{}{
				"type":  "content_block_start",
				"index": 0,
				"content_block": map[string]interface{}{
					"type": "text",
					"text": "",
				},
			})
			state["content_started"] = true
		}
	}

	finish := func(stopReason string) {
		if boolState(state, "stopped") {
			return
		}
		ensureStarted()
		writeEvent(map[string]interface{}{"type": "content_block_stop", "index": 0})
		writeEvent(map[string]interface{}{
			"type": "message_delta",
			"delta": map[string]interface{}{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": map[string]interface{}{
				"output_tokens": int64State(state, "output_tokens", totalOutput),
			},
		})
		writeEvent(map[string]interface{}{"type": "message_stop"})
		state["stopped"] = true
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			finish("end_turn")
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		if usage, ok := event["usageMetadata"].(map[string]interface{}); ok {
			totalInput = numberToInt64(usage["promptTokenCount"])
			totalOutput = numberToInt64(usage["candidatesTokenCount"])
			state["input_tokens"] = totalInput
			state["output_tokens"] = totalOutput
		}

		candidates, _ := event["candidates"].([]interface{})
		for _, rawCandidate := range candidates {
			candidate, _ := rawCandidate.(map[string]interface{})
			content, _ := candidate["content"].(map[string]interface{})
			parts, _ := content["parts"].([]interface{})
			for _, rawPart := range parts {
				part, _ := rawPart.(map[string]interface{})
				if text, ok := part["text"].(string); ok && text != "" {
					ensureStarted()
					writeEvent(map[string]interface{}{
						"type":  "content_block_delta",
						"index": 0,
						"delta": map[string]interface{}{
							"type": "text_delta",
							"text": text,
						},
					})
				}
			}

			if finishReason, ok := candidate["finishReason"].(string); ok && finishReason != "" {
				finish(geminiFinishReasonToAnthropic(finishReason))
			}
		}
	}

	if result.Len() == 0 {
		return nil, int64State(state, "input_tokens", totalInput), int64State(state, "output_tokens", totalOutput), nil
	}
	return result.Bytes(), int64State(state, "input_tokens", totalInput), int64State(state, "output_tokens", totalOutput), nil
}

func geminiFinishReasonToAnthropic(reason string) string {
	switch reason {
	case "MAX_TOKENS":
		return "max_tokens"
	case "STOP":
		return "end_turn"
	default:
		return "end_turn"
	}
}

func (a *GeminiAdapter) BuildMessagesRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	strippedModel := stripProviderPrefix(modelID)

	geminiBody := make(map[string]interface{})

	var systemParts []map[string]interface{}
	var contents []map[string]interface{}

	if system, ok := body["system"].(string); ok {
		systemParts = append(systemParts, map[string]interface{}{"text": system})
	}
	if systemList, ok := body["system"].([]interface{}); ok {
		for _, s := range systemList {
			if sm, ok := s.(map[string]interface{}); ok {
				if text, ok := sm["text"].(string); ok {
					systemParts = append(systemParts, map[string]interface{}{"text": text})
				}
			}
		}
	}
	if len(systemParts) > 0 {
		geminiBody["system_instruction"] = map[string]interface{}{"parts": systemParts}
	}

	messages, _ := body["messages"].([]interface{})
	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		geminiRole := "user"
		if role == "assistant" {
			geminiRole = "model"
		}

		parts := convertAnthropicContentToGeminiParts(msg["content"])

		contents = append(contents, map[string]interface{}{
			"role":  geminiRole,
			"parts": parts,
		})
	}

	geminiBody["contents"] = contents

	if maxTokens, ok := body["max_tokens"]; ok {
		setGenConfig(geminiBody, "maxOutputTokens", maxTokens)
	}
	if stop, ok := body["stop_sequences"]; ok {
		setGenConfig(geminiBody, "stopSequences", stop)
	}
	if temp, ok := body["temperature"]; ok {
		setGenConfig(geminiBody, "temperature", temp)
	}
	if topP, ok := body["top_p"]; ok {
		setGenConfig(geminiBody, "topP", topP)
	}

	endpoint := "/models/" + strippedModel + ":generateContent"
	return endpoint, geminiBody, nil
}

func convertAnthropicContentToGeminiParts(content interface{}) []map[string]interface{} {
	switch c := content.(type) {
	case string:
		return []map[string]interface{}{{"text": c}}
	case []interface{}:
		var parts []map[string]interface{}
		for _, block := range c {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := b["type"].(string)
			switch blockType {
			case "text":
				if text, ok := b["text"].(string); ok {
					parts = append(parts, map[string]interface{}{"text": text})
				}
			case "image":
				if source, ok := b["source"].(map[string]interface{}); ok {
					parts = append(parts, map[string]interface{}{
						"inlineData": map[string]interface{}{
							"mimeType": source["media_type"],
							"data":     source["data"],
						},
					})
				}
			case "tool_use":
				parts = append(parts, map[string]interface{}{"text": "[tool_use: " + fmt.Sprint(b["name"]) + "]"})
			}
		}
		return parts
	}
	return []map[string]interface{}{{"text": fmt.Sprint(content)}}
}

func (a *GeminiAdapter) ParseMessagesResponse(body map[string]interface{}) (map[string]interface{}, error) {
	response := make(map[string]interface{})

	if id := fmt.Sprintf("msg_%d", time.Now().UnixNano()); true {
		response["id"] = id
	}
	response["type"] = "message"

	var textContent string
	var stopReason string = "end_turn"
	var inputTokens, outputTokens float64

	candidates, _ := body["candidates"].([]interface{})
	for _, rawCandidate := range candidates {
		candidate, _ := rawCandidate.(map[string]interface{})
		content, _ := candidate["content"].(map[string]interface{})
		parts, _ := content["parts"].([]interface{})
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]interface{})
			if text, ok := part["text"].(string); ok {
				textContent += text
			}
		}
		if fr, ok := candidate["finishReason"].(string); ok {
			switch fr {
			case "STOP":
				stopReason = "end_turn"
			case "MAX_TOKENS":
				stopReason = "max_tokens"
			default:
				stopReason = "end_turn"
			}
		}
	}

	response["content"] = []map[string]interface{}{
		{"type": "text", "text": textContent},
	}
	response["role"] = "assistant"
	response["stop_reason"] = stopReason

	if usage, ok := body["usageMetadata"].(map[string]interface{}); ok {
		if pt, ok := usage["promptTokenCount"].(float64); ok {
			inputTokens = pt
		}
		if ct, ok := usage["candidatesTokenCount"].(float64); ok {
			outputTokens = ct
		}
		response["usage"] = map[string]interface{}{
			"input_tokens":  int64(inputTokens),
			"output_tokens": int64(outputTokens),
		}
	}

	return response, nil
}
