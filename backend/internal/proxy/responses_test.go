package proxy

import (
	"testing"
)

func TestResponsesToChatBodyStringInput(t *testing.T) {
	body := map[string]interface{}{
		"model":             "openai/gpt-4o",
		"input":             "hello",
		"instructions":      "be nice",
		"max_output_tokens": float64(100),
		"temperature":       0.5,
		"stream":            true,
	}
	chat, err := responsesToChatBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v", chat["model"])
	}
	if chat["max_tokens"] != float64(100) {
		t.Errorf("max_tokens = %v", chat["max_tokens"])
	}
	if chat["temperature"] != 0.5 || chat["stream"] != true {
		t.Errorf("passthrough fields = %#v", chat)
	}
	messages := chat["messages"].([]map[string]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "be nice" {
		t.Errorf("first message = %#v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "hello" {
		t.Errorf("second message = %#v", messages[1])
	}
}

func TestResponsesToChatBodyInputArray(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "what's the weather?"},
		}},
		map[string]interface{}{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": map[string]interface{}{"city": "seoul"}},
		map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": "sunny"},
		map[string]interface{}{"type": "function_call", "call_id": "call_2", "name": "get_temperature", "arguments": `{"city":"seoul","unit":"c"}`},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": input})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := chat["messages"].([]map[string]interface{})
	if len(messages) != 4 {
		t.Fatalf("messages len = %d", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Errorf("messages[0] role = %v", messages[0]["role"])
	}
	parts := messages[0]["content"].([]map[string]interface{})
	if parts[0]["type"] != "text" || parts[0]["text"] != "what's the weather?" {
		t.Errorf("messages[0] content = %#v", messages[0]["content"])
	}
	if messages[1]["role"] != "assistant" {
		t.Errorf("messages[1] role = %v", messages[1]["role"])
	}
	toolCalls := messages[1]["tool_calls"].([]map[string]interface{})
	fn := toolCalls[0]["function"].(map[string]interface{})
	if fn["name"] != "get_weather" || fn["arguments"] != `{"city":"seoul"}` {
		t.Errorf("tool call = %#v", toolCalls[0])
	}
	if messages[2]["role"] != "tool" || messages[2]["tool_call_id"] != "call_1" || messages[2]["content"] != "sunny" {
		t.Errorf("messages[2] = %#v", messages[2])
	}
	stringFn := messages[3]["tool_calls"].([]map[string]interface{})[0]["function"].(map[string]interface{})
	if stringFn["arguments"] != `{"city":"seoul","unit":"c"}` {
		t.Errorf("string arguments = %#v", stringFn["arguments"])
	}
}

func TestResponsesToChatBodyInputImage(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "input_image", "image_url": "https://example.com/cat.png", "detail": "high"},
		}},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": input})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := chat["messages"].([]map[string]interface{})[0]["content"].([]map[string]interface{})
	img := parts[0]["image_url"].(map[string]interface{})
	if parts[0]["type"] != "image_url" || img["url"] != "https://example.com/cat.png" || img["detail"] != "high" {
		t.Errorf("image part = %#v", parts[0])
	}
}

func TestResponsesToChatBodyTools(t *testing.T) {
	tools := []interface{}{
		map[string]interface{}{"type": "function", "name": "get_weather", "description": "d", "strict": true, "parameters": map[string]interface{}{"type": "object"}},
		map[string]interface{}{"type": "web_search"},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": "hi", "tools": tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	converted := chat["tools"].([]map[string]interface{})
	if len(converted) != 1 {
		t.Fatalf("tools len = %d (web_search should be dropped)", len(converted))
	}
	fn := converted[0]["function"].(map[string]interface{})
	if converted[0]["type"] != "function" || fn["name"] != "get_weather" || fn["strict"] != true {
		t.Errorf("converted tool = %#v", converted[0])
	}
}

func TestResponsesToChatBodyInvalidInput(t *testing.T) {
	if _, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": 42}); err == nil {
		t.Errorf("expected error for numeric input")
	}
}

func TestChatResponseToResponses(t *testing.T) {
	chat := map[string]interface{}{
		"id": "cmpl-1",
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "hello",
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":       "call_9",
							"type":     "function",
							"function": map[string]interface{}{"name": "get_weather", "arguments": `{"city":"seoul"}`},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]interface{}{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7},
	}
	resp := chatResponseToResponses(chat, "openai/gpt-4o")

	if resp["object"] != "response" || resp["model"] != "openai/gpt-4o" || resp["status"] != "completed" {
		t.Errorf("base fields = %#v", resp)
	}
	if resp["output_text"] != "hello" {
		t.Errorf("output_text = %v", resp["output_text"])
	}
	output := resp["output"].([]map[string]interface{})
	if len(output) != 2 {
		t.Fatalf("output len = %d", len(output))
	}
	if output[0]["type"] != "message" || output[1]["type"] != "function_call" {
		t.Errorf("output types = %v, %v", output[0]["type"], output[1]["type"])
	}
	fc := output[1]
	if fc["call_id"] != "call_9" || fc["name"] != "get_weather" || fc["arguments"] != `{"city":"seoul"}` {
		t.Errorf("function_call = %#v", fc)
	}
	usage := resp["usage"].(map[string]interface{})
	if usage["input_tokens"] != int64(4) || usage["output_tokens"] != int64(3) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestChatResponseToResponsesIncomplete(t *testing.T) {
	chat := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"message":       map[string]interface{}{"role": "assistant", "content": "part"},
			"finish_reason": "length",
		}},
	}
	resp := chatResponseToResponses(chat, "openai/gpt-4o")
	if resp["status"] != "incomplete" {
		t.Errorf("status = %v", resp["status"])
	}
}
