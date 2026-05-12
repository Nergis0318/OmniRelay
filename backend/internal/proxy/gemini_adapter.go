package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type GeminiAdapter struct{}

func (a *GeminiAdapter) BuildChatRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	strippedModel := modelID
	for i := 0; i < len(modelID); i++ {
		if modelID[i] == '/' {
			strippedModel = modelID[i+1:]
			break
		}
	}

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
		geminiBody["generationConfig"] = map[string]interface{}{
			"temperature": temp,
		}
	}
	if maxTokens, ok := body["max_tokens"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["maxOutputTokens"] = maxTokens
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{
				"maxOutputTokens": maxTokens,
			}
		}
	}
	if maxTokens, ok := body["max_completion_tokens"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["maxOutputTokens"] = maxTokens
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{
				"maxOutputTokens": maxTokens,
			}
		}
	}
	if topP, ok := body["top_p"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["topP"] = topP
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{
				"topP": topP,
			}
		}
	}
	if n, ok := body["n"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["candidateCount"] = n
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{
				"candidateCount": n,
			}
		}
	}
	if stop, ok := body["stop"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["stopSequences"] = stop
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{
				"stopSequences": stop,
			}
		}
	}
	if responseFormat, ok := body["response_format"].(map[string]interface{}); ok {
		if formatType, _ := responseFormat["type"].(string); formatType == "json_object" || formatType == "json_schema" {
			if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
				gc["responseMimeType"] = "application/json"
			} else {
				geminiBody["generationConfig"] = map[string]interface{}{
					"responseMimeType": "application/json",
				}
			}
		}
	}

	endpoint := "/models/" + strippedModel + ":generateContent"
	return endpoint, geminiBody, nil
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

func (a *GeminiAdapter) ParseStreamChunk(data []byte) ([]byte, error) {
	text := strings.TrimSpace(string(data))

	var result bytes.Buffer
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
				if text, ok := part["text"].(string); ok {
					deltaText += text
				}
			}
		}

		chunk := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"delta": map[string]interface{}{
						"content": deltaText,
					},
				},
			},
		}

		if usage, ok := event["usageMetadata"].(map[string]interface{}); ok {
			chunk["usage"] = map[string]interface{}{
				"prompt_tokens":     int64(0),
				"completion_tokens": int64(0),
				"total_tokens":      int64(0),
				"_gemini_usage":     usage,
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
				{
					"index": 0,
					"delta": map[string]interface{}{},
				},
			},
		})
		result.WriteString("data: ")
		result.Write(jsonChunk)
		result.WriteString("\n\n")
	}

	return result.Bytes(), nil
}

func (a *GeminiAdapter) FetchModels(apiBaseURL string, apiKey string) ([]string, error) {
	url := apiBaseURL + "/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-goog-api-key", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var modelIDs []string
	for _, m := range result.Models {
		name := m.Name
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		modelIDs = append(modelIDs, name)
	}
	return modelIDs, nil
}

func (a *GeminiAdapter) SupportsEndpoint(path string) bool {
	return true
}

func (a *GeminiAdapter) BuildMessagesRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	strippedModel := modelID
	for i := 0; i < len(modelID); i++ {
		if modelID[i] == '/' {
			strippedModel = modelID[i+1:]
			break
		}
	}

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
		geminiBody["generationConfig"] = map[string]interface{}{"maxOutputTokens": maxTokens}
	}
	if stop, ok := body["stop_sequences"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["stopSequences"] = stop
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{"stopSequences": stop}
		}
	}
	if temp, ok := body["temperature"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["temperature"] = temp
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{"temperature": temp}
		}
	}
	if topP, ok := body["top_p"]; ok {
		if gc, ok := geminiBody["generationConfig"].(map[string]interface{}); ok {
			gc["topP"] = topP
		} else {
			geminiBody["generationConfig"] = map[string]interface{}{"topP": topP}
		}
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

func (a *GeminiAdapter) IsSameFormat() bool {
	return false
}
