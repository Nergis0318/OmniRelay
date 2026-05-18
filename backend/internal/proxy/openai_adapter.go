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

type OpenAIAdapter struct{}

func (a *OpenAIAdapter) BuildChatRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	body["model"] = stripProviderPrefix(modelID)
	return "/chat/completions", body, nil
}

func (a *OpenAIAdapter) ParseChatResponse(body map[string]interface{}) (map[string]interface{}, error) {
	return body, nil
}

func (a *OpenAIAdapter) ParseStreamChunk(data []byte) ([]byte, int64, int64, error) {
	return data, 0, 0, nil
}

func (a *OpenAIAdapter) ParseMessagesStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error) {
	text := strings.TrimSpace(string(data))
	var result bytes.Buffer
	var inputTokens, outputTokens int64

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
				"output_tokens": int64State(state, "output_tokens", outputTokens),
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

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}

		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			inputTokens = numberToInt64(usage["prompt_tokens"])
			outputTokens = numberToInt64(usage["completion_tokens"])
			state["input_tokens"] = inputTokens
			state["output_tokens"] = outputTokens
		}

		choices, _ := chunk["choices"].([]interface{})
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]interface{})
			delta, _ := choice["delta"].(map[string]interface{})
			if content, ok := delta["content"].(string); ok && content != "" {
				ensureStarted()
				writeEvent(map[string]interface{}{
					"type":  "content_block_delta",
					"index": 0,
					"delta": map[string]interface{}{
						"type": "text_delta",
						"text": content,
					},
				})
			} else if _, ok := delta["role"].(string); ok {
				ensureStarted()
			}

			if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
				finish(openAIFinishReasonToAnthropic(finishReason))
			}
		}
	}

	if result.Len() == 0 {
		return nil, int64State(state, "input_tokens", inputTokens), int64State(state, "output_tokens", outputTokens), nil
	}
	return result.Bytes(), int64State(state, "input_tokens", inputTokens), int64State(state, "output_tokens", outputTokens), nil
}

func boolState(state map[string]interface{}, key string) bool {
	v, _ := state[key].(bool)
	return v
}

func int64State(state map[string]interface{}, key string, fallback int64) int64 {
	if v, ok := state[key].(int64); ok {
		return v
	}
	return fallback
}

func openAIFinishReasonToAnthropic(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
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

func (a *OpenAIAdapter) BuildMessagesRequest(body map[string]interface{}) (string, map[string]interface{}, error) {
	modelID, ok := body["model"].(string)
	if !ok {
		return "", nil, fmt.Errorf("model field is required")
	}

	body["model"] = stripProviderPrefix(modelID)

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
	if stream, ok := body["stream"]; ok {
		openaiBody["stream"] = stream
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
