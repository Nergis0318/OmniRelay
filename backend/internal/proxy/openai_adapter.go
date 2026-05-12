package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OpenAIAdapter struct{}

func (a *OpenAIAdapter) BuildChatRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	for i := 0; i < len(modelID); i++ {
		if modelID[i] == '/' {
			stripped := modelID[i+1:]
			body["model"] = stripped
			return "/chat/completions", body, nil
		}
	}

	return "/chat/completions", body, nil
}

func (a *OpenAIAdapter) ParseChatResponse(body map[string]interface{}) (map[string]interface{}, error) {
	return body, nil
}

func (a *OpenAIAdapter) ParseStreamChunk(data []byte) ([]byte, error) {
	return data, nil
}

func (a *OpenAIAdapter) FetchModels(apiBaseURL string, apiKey string) ([]string, error) {
	url := apiBaseURL + "/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

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

func (a *OpenAIAdapter) SupportsEndpoint(path string) bool {
	return true
}

func (a *OpenAIAdapter) BuildMessagesRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	for i := 0; i < len(modelID); i++ {
		if modelID[i] == '/' {
			body["model"] = modelID[i+1:]
			break
		}
	}

	openaiBody := make(map[string]interface{})
	openaiBody["model"] = body["model"]

	if maxTokens, ok := body["max_tokens"]; ok {
		openaiBody["max_tokens"] = maxTokens
	}
	if temp, ok := body["temperature"]; ok {
		openaiBody["temperature"] = temp
	}
	if topP, ok := body["top_p"]; ok {
		openaiBody["top_p"] = topP
	}
	if stop, ok := body["stop_sequences"]; ok {
		openaiBody["stop"] = stop
	}

	if system, ok := body["system"].(string); ok && system != "" {
		openaiBody["messages"] = []map[string]interface{}{
			{"role": "system", "content": system},
		}
	} else {
		openaiBody["messages"] = []map[string]interface{}{}
	}

	messages, _ := body["messages"].([]interface{})
	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content := convertAnthropicContentToText(msg["content"])
		if existing, ok := openaiBody["messages"].([]map[string]interface{}); ok {
			openaiBody["messages"] = append(existing, map[string]interface{}{
				"role":    role,
				"content": content,
			})
		}
	}

	return "/chat/completions", openaiBody, nil
}

func convertAnthropicContentToText(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		for _, block := range c {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if b["type"] == "text" {
				if text, ok := b["text"].(string); ok {
					return text
				}
			}
		}
	}
	return ""
}

func (a *OpenAIAdapter) ParseMessagesResponse(body map[string]interface{}) (map[string]interface{}, error) {
	response := make(map[string]interface{})

	if id, ok := body["id"]; ok {
		response["id"] = id
	}
	if model, ok := body["model"]; ok {
		response["model"] = model
	}
	response["type"] = "message"

	var textContent string
	var stopReason string

	choices, _ := body["choices"].([]interface{})
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})
		if content, ok := msg["content"].(string); ok {
			textContent += content
		}
		if fr, ok := choice["finish_reason"].(string); ok {
			switch fr {
			case "stop":
				stopReason = "end_turn"
			case "length":
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

	if usage, ok := body["usage"].(map[string]interface{}); ok {
		anthropicUsage := make(map[string]interface{})
		if pt, ok := usage["prompt_tokens"]; ok {
			anthropicUsage["input_tokens"] = pt
		}
		if ct, ok := usage["completion_tokens"]; ok {
			anthropicUsage["output_tokens"] = ct
		}
		response["usage"] = anthropicUsage
	}

	return response, nil
}

func (a *OpenAIAdapter) IsSameFormat() bool {
	return true
}
