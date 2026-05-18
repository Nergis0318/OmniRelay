package proxy

import (
	"testing"
)

func TestOpenAIParseMessagesResponseConvertsToAnthropicShape(t *testing.T) {
	adapter := &OpenAIAdapter{}
	body := map[string]interface{}{
		"id":    "chatcmpl-xyz",
		"model": "gpt-4o-2024-08-06",
		"choices": []interface{}{
			map[string]interface{}{
				"message":       map[string]interface{}{"content": "hello"},
				"finish_reason": "length",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     float64(5),
			"completion_tokens": float64(7),
		},
	}

	got, err := adapter.ParseMessagesResponse(body)
	if err != nil {
		t.Fatalf("ParseMessagesResponse: %v", err)
	}

	if got["id"] != "chatcmpl-xyz" {
		t.Errorf("id = %v, want chatcmpl-xyz", got["id"])
	}
	if got["type"] != "message" {
		t.Errorf("type = %v, want message", got["type"])
	}
	if got["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", got["role"])
	}
	if got["stop_reason"] != "max_tokens" {
		t.Errorf("stop_reason = %v, want max_tokens (length → max_tokens)", got["stop_reason"])
	}

	content, ok := got["content"].([]map[string]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("content shape unexpected: %#v", got["content"])
	}
	if content[0]["type"] != "text" || content[0]["text"] != "hello" {
		t.Errorf("content[0] = %#v, want {type:text, text:hello}", content[0])
	}

	usage, ok := got["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage shape unexpected: %#v", got["usage"])
	}
	if usage["input_tokens"] != float64(5) {
		t.Errorf("usage.input_tokens = %v, want 5", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(7) {
		t.Errorf("usage.output_tokens = %v, want 7", usage["output_tokens"])
	}
}

func TestOpenAIParseMessagesResponseDefaultsStopReasonForUnknownFinish(t *testing.T) {
	adapter := &OpenAIAdapter{}
	body := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{
				"message":       map[string]interface{}{"content": "x"},
				"finish_reason": "content_filter",
			},
		},
	}
	got, _ := adapter.ParseMessagesResponse(body)
	if got["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason = %v, want end_turn (default)", got["stop_reason"])
	}
}

func TestAnthropicParseChatResponseConvertsToOpenAIShape(t *testing.T) {
	adapter := &AnthropicAdapter{}
	body := map[string]interface{}{
		"id":          "msg_abc",
		"model":       "claude-sonnet-4-6",
		"role":        "assistant",
		"stop_reason": "max_tokens",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "hello "},
			map[string]interface{}{"type": "text", "text": "world"},
		},
		"usage": map[string]interface{}{
			"input_tokens":                float64(11),
			"output_tokens":               float64(4),
			"cache_creation_input_tokens": float64(2),
			"cache_read_input_tokens":     float64(8),
		},
	}

	got, err := adapter.ParseChatResponse(body)
	if err != nil {
		t.Fatalf("ParseChatResponse: %v", err)
	}

	if got["id"] != "msg_abc" {
		t.Errorf("id = %v, want msg_abc", got["id"])
	}
	if got["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", got["object"])
	}

	choices, ok := got["choices"].([]map[string]interface{})
	if !ok || len(choices) != 1 {
		t.Fatalf("choices shape unexpected: %#v", got["choices"])
	}
	choice := choices[0]
	if choice["finish_reason"] != "length" {
		t.Errorf("finish_reason = %v, want length (max_tokens → length)", choice["finish_reason"])
	}
	msg, ok := choice["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("message shape unexpected: %#v", choice["message"])
	}
	if msg["role"] != "assistant" {
		t.Errorf("message.role = %v, want assistant", msg["role"])
	}
	if msg["content"] != "hello world" {
		t.Errorf("message.content = %v, want \"hello world\"", msg["content"])
	}

	usage, ok := got["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("usage shape unexpected: %#v", got["usage"])
	}
	if usage["prompt_tokens"] != int64(11) {
		t.Errorf("prompt_tokens = %v (%T), want 11", usage["prompt_tokens"], usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != int64(4) {
		t.Errorf("completion_tokens = %v, want 4", usage["completion_tokens"])
	}
	if usage["total_tokens"] != int64(15) {
		t.Errorf("total_tokens = %v, want 15", usage["total_tokens"])
	}
	if usage["cache_creation_input_tokens"] != float64(2) {
		t.Errorf("cache_creation_input_tokens lost: got %v", usage["cache_creation_input_tokens"])
	}
	if usage["cache_read_input_tokens"] != float64(8) {
		t.Errorf("cache_read_input_tokens lost: got %v", usage["cache_read_input_tokens"])
	}
}

func TestAnthropicParseChatResponseFinishReasonMapping(t *testing.T) {
	adapter := &AnthropicAdapter{}
	cases := map[string]string{
		"end_turn":      "stop",
		"max_tokens":    "length",
		"stop_sequence": "stop",
		"tool_use":      "tool_calls",
		"":              "stop", // default
	}
	for stopReason, want := range cases {
		body := map[string]interface{}{
			"stop_reason": stopReason,
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": "x"}},
		}
		got, _ := adapter.ParseChatResponse(body)
		choices := got["choices"].([]map[string]interface{})
		if got := choices[0]["finish_reason"]; got != want {
			t.Errorf("stop_reason %q → finish_reason %q, want %q", stopReason, got, want)
		}
	}
}

func TestGeminiParseChatResponseConvertsToOpenAIShape(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{"text": "hello"},
						map[string]interface{}{"text": " world"},
					},
				},
				"finishReason": "MAX_TOKENS",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(3),
			"candidatesTokenCount": float64(2),
			"totalTokenCount":      float64(5),
		},
	}
	got, err := adapter.ParseChatResponse(body)
	if err != nil {
		t.Fatalf("ParseChatResponse: %v", err)
	}

	choices, ok := got["choices"].([]map[string]interface{})
	if !ok || len(choices) != 1 {
		t.Fatalf("choices shape unexpected: %#v", got["choices"])
	}
	if choices[0]["finish_reason"] != "length" {
		t.Errorf("MAX_TOKENS should map to length, got %v", choices[0]["finish_reason"])
	}
	msg := choices[0]["message"].(map[string]interface{})
	if msg["content"] != "hello world" {
		t.Errorf("content = %v, want \"hello world\"", msg["content"])
	}

	usage := got["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != int64(3) || usage["completion_tokens"] != int64(2) || usage["total_tokens"] != int64(5) {
		t.Errorf("usage = %#v, want prompt=3 completion=2 total=5", usage)
	}
}

func TestGeminiParseMessagesResponseConvertsToAnthropicShape(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{"text": "hi"},
					},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(7),
			"candidatesTokenCount": float64(2),
		},
	}

	got, err := adapter.ParseMessagesResponse(body)
	if err != nil {
		t.Fatalf("ParseMessagesResponse: %v", err)
	}
	if got["type"] != "message" {
		t.Errorf("type = %v, want message", got["type"])
	}
	if got["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", got["role"])
	}
	if got["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", got["stop_reason"])
	}
	content := got["content"].([]map[string]interface{})
	if content[0]["text"] != "hi" {
		t.Errorf("content[0].text = %v, want hi", content[0]["text"])
	}
	usage := got["usage"].(map[string]interface{})
	if usage["input_tokens"] != int64(7) {
		t.Errorf("input_tokens = %v, want 7", usage["input_tokens"])
	}
	if usage["output_tokens"] != int64(2) {
		t.Errorf("output_tokens = %v, want 2", usage["output_tokens"])
	}
}
