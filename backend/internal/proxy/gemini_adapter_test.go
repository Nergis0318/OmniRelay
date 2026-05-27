package proxy

import (
	"strings"
	"testing"
)

func TestGeminiParseStreamChunkConvertsToOpenAIChunks(t *testing.T) {
	adapter := &GeminiAdapter{}
	state := make(map[string]interface{})
	chunk := `data: {"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}` + "\n\n" +
		`data: {"candidates":[{"content":{"parts":[{"text":" world"}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2}}` + "\n\n"
	got, inputTokens, outputTokens, err := adapter.ParseStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("ParseStreamChunk: %v", err)
	}
	if inputTokens != 4 || outputTokens != 2 {
		t.Errorf("tokens = (%d, %d), want (4, 2)", inputTokens, outputTokens)
	}

	text := string(got)
	if strings.Count(text, `"chat.completion.chunk"`) != 2 {
		t.Errorf("expected 2 chat.completion.chunk records, got:\n%s", text)
	}
	if !strings.Contains(text, `"content":"hello"`) {
		t.Errorf("missing first delta hello in:\n%s", text)
	}
	if !strings.Contains(text, `"content":" world"`) {
		t.Errorf("missing second delta \" world\" in:\n%s", text)
	}
	if !strings.Contains(text, `"prompt_tokens":4`) || !strings.Contains(text, `"total_tokens":6`) {
		t.Errorf("missing usage block (prompt=4, total=6) in:\n%s", text)
	}
}

func TestGeminiParseStreamChunkEmitsPlaceholderWhenNoCandidates(t *testing.T) {
	adapter := &GeminiAdapter{}
	state := make(map[string]interface{})
	got, inputTokens, outputTokens, err := adapter.ParseStreamChunk([]byte(""), state)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if inputTokens != 0 || outputTokens != 0 {
		t.Errorf("empty input: tokens should be zero, got (%d, %d)", inputTokens, outputTokens)
	}
	text := string(got)
	if strings.Count(text, `"chat.completion.chunk"`) != 1 {
		t.Errorf("expected exactly one placeholder chunk, got:\n%s", text)
	}
	if strings.Contains(text, `"content"`) {
		t.Errorf("placeholder should have empty delta (no content key), got:\n%s", text)
	}
}

func TestGeminiParseStreamChunkSkipsDoneSentinel(t *testing.T) {
	adapter := &GeminiAdapter{}
	state := make(map[string]interface{})
	got, _, _, err := adapter.ParseStreamChunk([]byte("data: [DONE]\n\n"), state)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Count(string(got), `"chat.completion.chunk"`) != 1 {
		t.Errorf("[DONE] alone should produce one placeholder chunk, got:\n%s", got)
	}
}

func TestGeminiParseMessagesStreamChunkConvertsToAnthropicEvents(t *testing.T) {
	adapter := &GeminiAdapter{}
	state := make(map[string]interface{})

	chunk := `data: {"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}` + "\n\n" +
		`data: {"candidates":[{"content":{"parts":[{"text":" world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2}}` + "\n\n"

	got, inputTokens, outputTokens, err := adapter.ParseMessagesStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("ParseMessagesStreamChunk returned error: %v", err)
	}

	text := string(got)
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		`"text":"hello"`,
		`"text":" world"`,
		"event: content_block_stop",
		"event: message_delta",
		`"stop_reason":"end_turn"`,
		"event: message_stop",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("converted stream missing %q in:\n%s", want, text)
		}
	}

	if inputTokens != 4 || outputTokens != 2 {
		t.Fatalf("tokens = (%d, %d), want (4, 2)", inputTokens, outputTokens)
	}
}

func TestGeminiParseMessagesStreamChunkMaxTokensMapping(t *testing.T) {
	adapter := &GeminiAdapter{}
	state := make(map[string]interface{})

	chunk := `data: {"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"MAX_TOKENS"}]}` + "\n\n"
	got, _, _, err := adapter.ParseMessagesStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(string(got), `"stop_reason":"max_tokens"`) {
		t.Fatalf("MAX_TOKENS should map to max_tokens in:\n%s", got)
	}
}

func TestGeminiParseMessagesStreamChunkKeepsStateAcrossChunks(t *testing.T) {
	adapter := &GeminiAdapter{}
	state := make(map[string]interface{})

	first, _, _, err := adapter.ParseMessagesStreamChunk([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"a"}]}}]}`+"\n\n"), state)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, _, _, err := adapter.ParseMessagesStreamChunk([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"b"}]}}]}`+"\n\n"), state)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	combined := string(first) + string(second)
	if strings.Count(combined, "event: message_start") != 1 {
		t.Fatalf("message_start should only appear once across chunks; got:\n%s", combined)
	}
	if strings.Count(combined, "event: content_block_start") != 1 {
		t.Fatalf("content_block_start should only appear once across chunks; got:\n%s", combined)
	}
}

func TestGeminiParseMessagesStreamChunkDoneEvent(t *testing.T) {
	adapter := &GeminiAdapter{}
	state := make(map[string]interface{})

	// Open with content first
	if _, _, _, err := adapter.ParseMessagesStreamChunk([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"x"}]}}]}`+"\n\n"), state); err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _, _, err := adapter.ParseMessagesStreamChunk([]byte("data: [DONE]\n\n"), state)
	if err != nil {
		t.Fatalf("done: %v", err)
	}
	text := string(got)
	for _, want := range []string{
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("[DONE] should emit %q in:\n%s", want, text)
		}
	}
}
