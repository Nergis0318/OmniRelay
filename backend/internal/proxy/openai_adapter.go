package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
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

func (a *OpenAIAdapter) ParseStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error) {
	text := strings.TrimSpace(string(data))
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

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}

		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			inputTokens = numberToInt64(usage["prompt_tokens"])
			outputTokens = numberToInt64(usage["completion_tokens"])
			state["input_tokens"] = inputTokens
			state["output_tokens"] = outputTokens
			extractAndStoreCacheTokens(usage, state)
		}

		if choices, ok := chunk["choices"].([]interface{}); ok {
			for _, rawChoice := range choices {
				choice, _ := rawChoice.(map[string]interface{})
				delta, _ := choice["delta"].(map[string]interface{})
				if content, ok := delta["content"].(string); ok && isUpstreamErrorContent(content) {
					state["upstream_error"] = content
					return nil, int64State(state, "input_tokens", inputTokens), int64State(state, "output_tokens", outputTokens), nil
				}
			}
		}
	}

	return data, int64State(state, "input_tokens", inputTokens), int64State(state, "output_tokens", outputTokens), nil
}

func (a *OpenAIAdapter) ParseMessagesStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error) {
	text := strings.TrimSpace(string(data))
	w := newMessagesStreamWriter(state)
	var inputTokens, outputTokens int64

	// Track tool call indexes we've already started for this stream
	toolStarted := make(map[int]bool)
	if existing, ok := state["tool_call_indexes"].(map[int]bool); ok {
		toolStarted = existing
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			w.finish("end_turn", outputTokens)
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
			extractAndStoreCacheTokens(usage, state)
		}

		choices, _ := chunk["choices"].([]interface{})
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]interface{})
			delta, _ := choice["delta"].(map[string]interface{})

			// Handle tool_calls delta for streaming
			if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
				for _, rawTC := range toolCalls {
					tc, _ := rawTC.(map[string]interface{})
					tcIndex := int(numberToInt64(tc["index"]))
					tcID, hasID := tc["id"].(string)

					tcFn, _ := tc["function"].(map[string]interface{})
					fnName, _ := tcFn["name"].(string)
					fnArgs, _ := tcFn["arguments"].(string)

					if hasID && tcID != "" && !toolStarted[tcIndex] {
						toolStarted[tcIndex] = true
						w.ensureStarted()
						w.writeEvent(map[string]interface{}{
							"type":  "content_block_start",
							"index": tcIndex,
							"content_block": map[string]interface{}{
								"type":  "tool_use",
								"id":    tcID,
								"name":  fnName,
								"input": map[string]interface{}{},
							},
						})
						// If this first chunk also has arguments, emit the delta separately
						if fnArgs != "" {
							w.writeEvent(map[string]interface{}{
								"type":  "content_block_delta",
								"index": tcIndex,
								"delta": map[string]interface{}{
									"type":         "input_json_delta",
									"partial_json": fnArgs,
								},
							})
						}
					} else if fnArgs != "" && toolStarted[tcIndex] {
						// Ongoing tool call arguments
						w.writeEvent(map[string]interface{}{
							"type":  "content_block_delta",
							"index": tcIndex,
							"delta": map[string]interface{}{
								"type":         "input_json_delta",
								"partial_json": fnArgs,
							},
						})
					}
				}
				state["tool_call_indexes"] = toolStarted
				continue
			}

			if content, ok := delta["content"].(string); ok {
				if isUpstreamErrorContent(content) {
					state["upstream_error"] = content
					return nil, int64State(state, "input_tokens", inputTokens), int64State(state, "output_tokens", outputTokens), nil
				}
				if content != "" {
					w.textDelta(content)
				}
			} else if _, ok := delta["role"].(string); ok {
				w.ensureStarted()
			}

			if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
				w.finish(openAIFinishReasonToAnthropic(finishReason), outputTokens)
			}
		}
	}

	return w.bytes(), int64State(state, "input_tokens", inputTokens), int64State(state, "output_tokens", outputTokens), nil
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
		if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
			anthropicUsage["prompt_tokens_details"] = details
		}
		response["usage"] = anthropicUsage
	}

	return response, nil
}
