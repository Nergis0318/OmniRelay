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

type AnthropicAdapter struct{}

func (a *AnthropicAdapter) BuildChatRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	anthropicBody := make(map[string]interface{})

	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	anthropicBody["model"] = stripProviderPrefix(modelID)

	if maxTokens, ok := body["max_tokens"]; ok {
		anthropicBody["max_tokens"] = maxTokens
	} else if maxCompletion, ok := body["max_completion_tokens"]; ok {
		anthropicBody["max_tokens"] = maxCompletion
	} else if contextWindow, ok := body["_context_window"].(int64); ok && contextWindow > 0 {
		anthropicBody["max_tokens"] = contextWindow
	} else {
		anthropicBody["max_tokens"] = 4096
	}

	if temp, ok := body["temperature"]; ok {
		anthropicBody["temperature"] = temp
	}
	if topP, ok := body["top_p"]; ok {
		anthropicBody["top_p"] = topP
	}
	if stop, ok := body["stop"]; ok {
		anthropicBody["stop_sequences"] = stop
	}

	messages, ok := body["messages"].([]interface{})
	if !ok {
		return "", nil, fmt.Errorf("messages field is required")
	}

	var systemPrompt string
	var anthropicMessages []map[string]interface{}

	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content := msg["content"]
		name, _ := msg["name"].(string)

		if role == "system" {
			if contentStr, ok := content.(string); ok {
				if systemPrompt != "" {
					systemPrompt += "\n\n"
				}
				systemPrompt += contentStr
			}
			continue
		}

		anthropicMsg := map[string]interface{}{
			"role":    role,
			"content": convertContent(content),
		}
		if name != "" {
			anthropicMsg["name"] = name
		}
		anthropicMessages = append(anthropicMessages, anthropicMsg)
	}

	if systemPrompt != "" {
		anthropicBody["system"] = systemPrompt
	}

	anthropicBody["messages"] = anthropicMessages

	return "/v1/messages", anthropicBody, nil
}

func convertContent(content interface{}) interface{} {
	switch val := content.(type) {
	case string:
		return val
	case []interface{}:
		var anthropicContent []map[string]interface{}
		for _, part := range val {
			p, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			partType, _ := p["type"].(string)

			switch partType {
			case "text":
				anthropicContent = append(anthropicContent, map[string]interface{}{
					"type": "text",
					"text": p["text"],
				})
			case "image_url":
				imageURL, _ := p["image_url"].(map[string]interface{})
				url, _ := imageURL["url"].(string)
				if url != "" {
					dataParts := strings.SplitN(url, ",", 2)
					mediaType := "image/jpeg"
					if len(dataParts) == 2 && strings.HasPrefix(dataParts[0], "data:") {
						mediaType = strings.TrimSuffix(strings.TrimPrefix(dataParts[0], "data:"), ";base64")
					}
					imageData := dataParts[len(dataParts)-1]
					anthropicContent = append(anthropicContent, map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": mediaType,
							"data":       imageData,
						},
					})
				}
			}
		}
		return anthropicContent
	}
	return content
}

func (a *AnthropicAdapter) ParseChatResponse(body map[string]interface{}) (map[string]interface{}, error) {
	response := make(map[string]interface{})

	var textContent string
	var stopReason string
	var inputTokens, outputTokens float64

	if id, ok := body["id"]; ok {
		response["id"] = id
	}

	if model, ok := body["model"]; ok {
		response["model"] = model
	}

	if sr, ok := body["stop_reason"].(string); ok {
		stopReason = sr
	}

	if usage, ok := body["usage"].(map[string]interface{}); ok {
		if it, ok := usage["input_tokens"].(float64); ok {
			inputTokens = it
		}
		if ot, ok := usage["output_tokens"].(float64); ok {
			outputTokens = ot
		}
		openaiUsage := map[string]interface{}{
			"prompt_tokens":     int64(inputTokens),
			"completion_tokens": int64(outputTokens),
			"total_tokens":      int64(inputTokens + outputTokens),
		}
		if v, ok := usage["cache_creation_input_tokens"]; ok {
			openaiUsage["cache_creation_input_tokens"] = v
		}
		if v, ok := usage["cache_read_input_tokens"]; ok {
			openaiUsage["cache_read_input_tokens"] = v
		}
		response["usage"] = openaiUsage
	}

	if content, ok := body["content"].([]interface{}); ok {
		for _, block := range content {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if blockType, _ := b["type"].(string); blockType == "text" {
				if text, ok := b["text"].(string); ok {
					textContent += text
				}
			}
		}
	}

	finishReason := "stop"
	switch stopReason {
	case "end_turn":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	case "stop_sequence":
		finishReason = "stop"
	case "tool_use":
		finishReason = "tool_calls"
	}

	role := "assistant"
	if bodyRole, ok := body["role"].(string); ok {
		role = bodyRole
	}

	response["object"] = "chat.completion"
	response["created"] = time.Now().Unix()
	response["choices"] = []map[string]interface{}{
		{
			"index": 0,
			"message": map[string]interface{}{
				"role":    role,
				"content": textContent,
			},
			"finish_reason": finishReason,
		},
	}

	return response, nil
}

func (a *AnthropicAdapter) ParseStreamChunk(data []byte) ([]byte, int64, int64, error) {
	text := strings.TrimSpace(string(data))

	var events []map[string]interface{}
	var inputTokens, outputTokens int64

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

		eventType, _ := event["type"].(string)
		switch eventType {
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]interface{})
			if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
				textDelta, _ := delta["text"].(string)
				events = append(events, map[string]interface{}{
					"choices": []map[string]interface{}{
						{"index": 0, "delta": map[string]interface{}{"content": textDelta}},
					},
				})
			}
		case "message_start":
			if msg, ok := event["message"].(map[string]interface{}); ok {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					if v, ok := usage["input_tokens"].(float64); ok {
						inputTokens = int64(v)
					}
					events = append(events, map[string]interface{}{
						"choices": []map[string]interface{}{
							{"index": 0, "delta": map[string]interface{}{"role": "assistant"}},
						},
					})
				}
			}
		case "message_delta":
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				if v, ok := usage["output_tokens"].(float64); ok {
					outputTokens = int64(v)
				}
				events = append(events, map[string]interface{}{
					"choices": []map[string]interface{}{
						{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
					},
					"usage": map[string]interface{}{
						"prompt_tokens":     inputTokens,
						"completion_tokens": outputTokens,
						"total_tokens":      inputTokens + outputTokens,
					},
				})
			}
		}
	}

	var result bytes.Buffer
	for _, ev := range events {
		chunk := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
		}
		for k, v := range ev {
			chunk[k] = v
		}
		jsonChunk, _ := json.Marshal(chunk)
		result.WriteString("data: ")
		result.Write(jsonChunk)
		result.WriteString("\n\n")
	}

	if result.Len() > 0 {
		return result.Bytes(), inputTokens, outputTokens, nil
	}

	jsonChunk, _ := json.Marshal(map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"choices": []map[string]interface{}{
			{"index": 0, "delta": map[string]interface{}{}},
		},
	})
	var buf bytes.Buffer
	buf.WriteString("data: ")
	buf.Write(jsonChunk)
	buf.WriteString("\n\n")
	return buf.Bytes(), inputTokens, outputTokens, nil
}

func (a *AnthropicAdapter) ParseMessagesStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error) {
	return data, 0, 0, nil
}

func (a *AnthropicAdapter) FetchModels(apiBaseURL string, apiKey string) ([]string, error) {
	url := apiBaseURL + "/v1/models?limit=1000"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var modelIDs []string
	for _, m := range result.Data {
		modelIDs = append(modelIDs, m.ID)
	}
	return modelIDs, nil
}

func (a *AnthropicAdapter) BuildMessagesRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}
	body["model"] = stripProviderPrefix(modelID)
	return "/v1/messages", body, nil
}

func (a *AnthropicAdapter) ParseMessagesResponse(body map[string]interface{}) (map[string]interface{}, error) {
	return body, nil
}
