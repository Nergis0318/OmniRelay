package proxy

import (
	"encoding/json"
	"fmt"
)

// responsesToChatBody converts an OpenAI Responses API request body into a
// chat completions request body for the existing proxy pipeline.
func responsesToChatBody(body map[string]interface{}) (map[string]interface{}, error) {
	chat := make(map[string]interface{})

	for _, key := range []string{"model", "stream", "temperature", "top_p", "stop"} {
		if v, ok := body[key]; ok {
			chat[key] = v
		}
	}
	if v, ok := body["max_output_tokens"]; ok {
		chat["max_tokens"] = v
	}

	messages, err := inputToMessages(body["input"])
	if err != nil {
		return nil, err
	}
	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages = append([]map[string]interface{}{{"role": "system", "content": instructions}}, messages...)
	}
	chat["messages"] = messages

	if tools, ok := body["tools"].([]interface{}); ok {
		chat["tools"] = convertResponsesTools(tools)
	}
	return chat, nil
}

func inputToMessages(input interface{}) ([]map[string]interface{}, error) {
	if s, ok := input.(string); ok {
		return []map[string]interface{}{{"role": "user", "content": s}}, nil
	}
	items, ok := input.([]interface{})
	if !ok {
		return nil, fmt.Errorf("input must be a string or an array")
	}
	var messages []map[string]interface{}
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if msgType, _ := item["type"].(string); msgType != "" && msgType != "message" {
			switch msgType {
			case "function_call_output":
				messages = append(messages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": item["call_id"],
					"content":      item["output"],
				})
			case "function_call":
				args := "{}"
				if a, ok := item["arguments"]; ok {
					if b, err := json.Marshal(a); err == nil {
						args = string(b)
					}
				}
				messages = append(messages, map[string]interface{}{
					"role": "assistant",
					"tool_calls": []map[string]interface{}{{
						"id":   item["call_id"],
						"type": "function",
						"function": map[string]interface{}{
							"name":      item["name"],
							"arguments": args,
						},
					}},
				})
			}
			continue
		}
		role, _ := item["role"].(string)
		if role == "" {
			role = "user"
		}
		messages = append(messages, map[string]interface{}{
			"role":    role,
			"content": convertResponsesContent(item["content"]),
		})
	}
	return messages, nil
}

func convertResponsesContent(content interface{}) interface{} {
	if s, ok := content.(string); ok {
		return s
	}
	parts, ok := content.([]interface{})
	if !ok {
		return ""
	}
	out := make([]map[string]interface{}, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch part["type"] {
		case "input_text":
			out = append(out, map[string]interface{}{"type": "text", "text": part["text"]})
		case "input_image":
			out = append(out, map[string]interface{}{"type": "image_url", "image_url": part["image_url"]})
		default:
			out = append(out, part)
		}
	}
	return out
}

func convertResponsesTools(tools []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]interface{})
		if !ok || tool["type"] != "function" {
			continue
		}
		fn := make(map[string]interface{})
		for _, k := range []string{"name", "description", "parameters", "strict"} {
			if v, ok := tool[k]; ok {
				fn[k] = v
			}
		}
		out = append(out, map[string]interface{}{"type": "function", "function": fn})
	}
	return out
}
