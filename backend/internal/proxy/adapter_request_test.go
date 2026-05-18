package proxy

import (
	"testing"
)

func TestAnthropicBuildChatRequestExtractsSystemPrompt(t *testing.T) {
	adapter := &AnthropicAdapter{}
	body := map[string]interface{}{
		"model": "anthropic/claude-sonnet-4-6",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "you are helpful"},
			map[string]interface{}{"role": "system", "content": "be terse"},
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	endpoint, got, err := adapter.BuildChatRequest(body)
	if err != nil {
		t.Fatalf("BuildChatRequest: %v", err)
	}
	if endpoint != "/v1/messages" {
		t.Errorf("endpoint = %q, want /v1/messages", endpoint)
	}

	// Provider prefix is stripped.
	if got["model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v, want claude-sonnet-4-6", got["model"])
	}
	// Multiple system messages concatenate with a blank-line separator.
	if got["system"] != "you are helpful\n\nbe terse" {
		t.Errorf("system = %q, want \"you are helpful\\n\\nbe terse\"", got["system"])
	}

	msgs, ok := got["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages shape unexpected: %#v", got["messages"])
	}
	if len(msgs) != 1 || msgs[0]["role"] != "user" {
		t.Fatalf("only the non-system user message should remain: %#v", msgs)
	}
}

func TestAnthropicBuildChatRequestUsesContextWindowFallback(t *testing.T) {
	adapter := &AnthropicAdapter{}
	body := map[string]interface{}{
		"model":            "claude-sonnet-4-6",
		"_context_window": int64(200000),
		"messages":         []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	_, got, err := adapter.BuildChatRequest(body)
	if err != nil {
		t.Fatalf("BuildChatRequest: %v", err)
	}
	if got["max_tokens"] != int64(200000) {
		t.Errorf("max_tokens = %v, want 200000 (from _context_window)", got["max_tokens"])
	}
}

func TestAnthropicBuildChatRequestMaxTokensPrecedence(t *testing.T) {
	adapter := &AnthropicAdapter{}
	// Explicit max_tokens wins over max_completion_tokens and _context_window.
	body := map[string]interface{}{
		"model":                  "claude",
		"max_tokens":             4096,
		"max_completion_tokens":  8192,
		"_context_window":        int64(200000),
		"messages":               []interface{}{map[string]interface{}{"role": "user", "content": "x"}},
	}
	_, got, _ := adapter.BuildChatRequest(body)
	if got["max_tokens"] != 4096 {
		t.Errorf("max_tokens = %v, want 4096", got["max_tokens"])
	}
}

func TestAnthropicBuildChatRequestDefaultsMaxTokens(t *testing.T) {
	adapter := &AnthropicAdapter{}
	body := map[string]interface{}{
		"model":    "claude",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "x"}},
	}
	_, got, _ := adapter.BuildChatRequest(body)
	if got["max_tokens"] != 4096 {
		t.Errorf("default max_tokens = %v, want 4096", got["max_tokens"])
	}
}

func TestAnthropicBuildChatRequestConvertsImageURL(t *testing.T) {
	adapter := &AnthropicAdapter{}
	body := map[string]interface{}{
		"model": "claude",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "what is this?"},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:image/png;base64,iVBORw0KGgo=",
						},
					},
				},
			},
		},
	}
	_, got, err := adapter.BuildChatRequest(body)
	if err != nil {
		t.Fatalf("BuildChatRequest: %v", err)
	}
	msgs := got["messages"].([]map[string]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %#v", len(msgs), msgs)
	}
	content, ok := msgs[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("content shape unexpected: %#v", msgs[0]["content"])
	}
	if content[0]["type"] != "text" || content[0]["text"] != "what is this?" {
		t.Errorf("content[0] = %#v, want text/what is this?", content[0])
	}
	if content[1]["type"] != "image" {
		t.Fatalf("content[1] should be image, got %v", content[1])
	}
	source, ok := content[1]["source"].(map[string]interface{})
	if !ok {
		t.Fatalf("image.source missing: %#v", content[1])
	}
	if source["type"] != "base64" {
		t.Errorf("source.type = %v, want base64", source["type"])
	}
	if source["media_type"] != "image/png" {
		t.Errorf("source.media_type = %v, want image/png", source["media_type"])
	}
	if source["data"] != "iVBORw0KGgo=" {
		t.Errorf("source.data = %v, want raw base64 without prefix", source["data"])
	}
}

func TestAnthropicBuildChatRequestRequiresMessages(t *testing.T) {
	adapter := &AnthropicAdapter{}
	body := map[string]interface{}{"model": "claude"}
	if _, _, err := adapter.BuildChatRequest(body); err == nil {
		t.Fatal("expected error when messages field is missing")
	}
}

func TestOpenAIBuildMessagesRequestPrependsSystem(t *testing.T) {
	adapter := &OpenAIAdapter{}
	body := map[string]interface{}{
		"model":      "openai/gpt-4o",
		"system":     "you are helpful",
		"max_tokens": 100,
		"stream":     true,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	endpoint, got, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		t.Fatalf("BuildMessagesRequest: %v", err)
	}
	if endpoint != "/chat/completions" {
		t.Errorf("endpoint = %q, want /chat/completions", endpoint)
	}
	if got["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o (prefix stripped)", got["model"])
	}
	if got["max_tokens"] != 100 {
		t.Errorf("max_tokens = %v, want 100", got["max_tokens"])
	}
	if got["stream"] != true {
		t.Errorf("stream = %v, want true", got["stream"])
	}

	msgs, ok := got["messages"].([]map[string]interface{})
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages shape unexpected: %#v", got["messages"])
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "you are helpful" {
		t.Errorf("messages[0] = %#v, want system/you are helpful", msgs[0])
	}
	if msgs[1]["role"] != "user" || msgs[1]["content"] != "hi" {
		t.Errorf("messages[1] = %#v, want user/hi", msgs[1])
	}
}

func TestOpenAIBuildMessagesRequestRenamesStopSequencesAndOmitsEmptySystem(t *testing.T) {
	adapter := &OpenAIAdapter{}
	body := map[string]interface{}{
		"model":          "gpt-4o",
		"stop_sequences": []string{"###"},
		"messages":       []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	_, got, _ := adapter.BuildMessagesRequest(body)

	if _, present := got["stop_sequences"]; present {
		t.Error("stop_sequences should be renamed, not forwarded")
	}
	stop, ok := got["stop"].([]string)
	if !ok || len(stop) != 1 || stop[0] != "###" {
		t.Errorf("stop = %v, want [###]", got["stop"])
	}

	msgs := got["messages"].([]map[string]interface{})
	if len(msgs) != 1 || msgs[0]["role"] != "user" {
		t.Errorf("without system field, only user message should remain: %#v", msgs)
	}
}
