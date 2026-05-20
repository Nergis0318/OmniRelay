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

func TestGeminiBuildChatRequestExtractsSystemAndMapsRoles(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"model":       "gemini/gemini-2.0-flash",
		"temperature": 0.5,
		"top_p":       0.9,
		"max_tokens":  256,
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "you are helpful"},
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{"role": "assistant", "content": "hello"},
		},
	}
	endpoint, got, err := adapter.BuildChatRequest(body)
	if err != nil {
		t.Fatalf("BuildChatRequest: %v", err)
	}
	if endpoint != "/models/gemini-2.0-flash:generateContent" {
		t.Errorf("endpoint = %q, want /models/gemini-2.0-flash:generateContent (prefix stripped)", endpoint)
	}

	sys, ok := got["systemInstruction"].(map[string]interface{})
	if !ok {
		t.Fatalf("systemInstruction missing: %#v", got["systemInstruction"])
	}
	parts, ok := sys["parts"].([]map[string]interface{})
	if !ok || len(parts) != 1 || parts[0]["text"] != "you are helpful" {
		t.Errorf("systemInstruction.parts = %#v, want [{text:\"you are helpful\"}]", sys["parts"])
	}

	contents, ok := got["contents"].([]map[string]interface{})
	if !ok || len(contents) != 2 {
		t.Fatalf("contents shape unexpected: %#v", got["contents"])
	}
	if contents[0]["role"] != "user" {
		t.Errorf("contents[0].role = %v, want user", contents[0]["role"])
	}
	if contents[1]["role"] != "model" {
		t.Errorf("contents[1].role = %v, want model (assistant → model)", contents[1]["role"])
	}

	gc, ok := got["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("generationConfig missing: %#v", got["generationConfig"])
	}
	if gc["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", gc["temperature"])
	}
	if gc["topP"] != 0.9 {
		t.Errorf("topP = %v, want 0.9", gc["topP"])
	}
	if gc["maxOutputTokens"] != 256 {
		t.Errorf("maxOutputTokens = %v, want 256", gc["maxOutputTokens"])
	}
}

func TestGeminiBuildChatRequestConvertsInlineImage(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "describe"},
					map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": "data:image/jpeg;base64,/9j/4AAQ"},
					},
				},
			},
		},
	}
	_, got, err := adapter.BuildChatRequest(body)
	if err != nil {
		t.Fatalf("BuildChatRequest: %v", err)
	}
	contents := got["contents"].([]map[string]interface{})
	parts := contents[0]["parts"].([]map[string]interface{})
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image), got %d: %#v", len(parts), parts)
	}
	if parts[0]["text"] != "describe" {
		t.Errorf("parts[0].text = %v, want describe", parts[0]["text"])
	}
	inline, ok := parts[1]["inlineData"].(map[string]interface{})
	if !ok {
		t.Fatalf("parts[1].inlineData missing: %#v", parts[1])
	}
	if inline["mimeType"] != "image/jpeg" {
		t.Errorf("inlineData.mimeType = %v, want image/jpeg", inline["mimeType"])
	}
	if inline["data"] != "/9j/4AAQ" {
		t.Errorf("inlineData.data = %v, want raw base64 without prefix", inline["data"])
	}
}

func TestGeminiBuildChatRequestConvertsRemoteImageAsFileData(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": "https://example.com/cat.png"},
					},
				},
			},
		},
	}
	_, got, _ := adapter.BuildChatRequest(body)
	parts := got["contents"].([]map[string]interface{})[0]["parts"].([]map[string]interface{})
	fd, ok := parts[0]["fileData"].(map[string]interface{})
	if !ok {
		t.Fatalf("non-data: URL should become fileData, got %#v", parts[0])
	}
	if fd["fileUri"] != "https://example.com/cat.png" {
		t.Errorf("fileData.fileUri = %v, want full URL", fd["fileUri"])
	}
}

func TestGeminiBuildChatRequestResponseFormatMapsToJSON(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"model":    "gemini",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "x"}},
		"response_format": map[string]interface{}{
			"type": "json_object",
		},
	}
	_, got, _ := adapter.BuildChatRequest(body)
	gc, ok := got["generationConfig"].(map[string]interface{})
	if !ok || gc["responseMimeType"] != "application/json" {
		t.Errorf("response_format=json_object should set responseMimeType, got %v", got["generationConfig"])
	}

	// response_format with unsupported type should NOT touch responseMimeType.
	body["response_format"] = map[string]interface{}{"type": "text"}
	_, got, _ = adapter.BuildChatRequest(body)
	if gc, ok := got["generationConfig"].(map[string]interface{}); ok {
		if _, present := gc["responseMimeType"]; present {
			t.Errorf("response_format=text should not set responseMimeType, got %v", gc)
		}
	}
}

func TestGeminiBuildMessagesRequestExtractsSystemAndMapsRoles(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"model":      "gemini/gemini-2.0-flash",
		"system":     "you are helpful",
		"max_tokens": 256,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{"role": "assistant", "content": "hello"},
		},
	}
	endpoint, got, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		t.Fatalf("BuildMessagesRequest: %v", err)
	}
	if endpoint != "/models/gemini-2.0-flash:generateContent" {
		t.Errorf("endpoint = %q, want /models/gemini-2.0-flash:generateContent", endpoint)
	}

	sys, ok := got["systemInstruction"].(map[string]interface{})
	if !ok {
		t.Fatalf("systemInstruction missing: %#v", got["systemInstruction"])
	}
	parts := sys["parts"].([]map[string]interface{})
	if len(parts) != 1 || parts[0]["text"] != "you are helpful" {
		t.Errorf("systemInstruction.parts = %#v", parts)
	}

	contents := got["contents"].([]map[string]interface{})
	if len(contents) != 2 {
		t.Fatalf("contents len = %d, want 2", len(contents))
	}
	if contents[0]["role"] != "user" || contents[1]["role"] != "model" {
		t.Errorf("roles = (%v, %v), want (user, model)", contents[0]["role"], contents[1]["role"])
	}

	gc := got["generationConfig"].(map[string]interface{})
	if gc["maxOutputTokens"] != 256 {
		t.Errorf("maxOutputTokens = %v, want 256", gc["maxOutputTokens"])
	}
}

func TestGeminiBuildMessagesRequestHandlesAnthropicContentBlocks(t *testing.T) {
	adapter := &GeminiAdapter{}
	body := map[string]interface{}{
		"model": "gemini",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "what is this?"},
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"media_type": "image/png",
							"data":       "iVBORw==",
						},
					},
				},
			},
		},
	}
	_, got, err := adapter.BuildMessagesRequest(body)
	if err != nil {
		t.Fatalf("BuildMessagesRequest: %v", err)
	}
	parts := got["contents"].([]map[string]interface{})[0]["parts"].([]map[string]interface{})
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %#v", len(parts), parts)
	}
	if parts[0]["text"] != "what is this?" {
		t.Errorf("parts[0].text = %v", parts[0]["text"])
	}
	inline, ok := parts[1]["inlineData"].(map[string]interface{})
	if !ok {
		t.Fatalf("parts[1].inlineData missing: %#v", parts[1])
	}
	if inline["mimeType"] != "image/png" {
		t.Errorf("inlineData.mimeType = %v, want image/png", inline["mimeType"])
	}
	if inline["data"] != "iVBORw==" {
		t.Errorf("inlineData.data = %v", inline["data"])
	}
}
