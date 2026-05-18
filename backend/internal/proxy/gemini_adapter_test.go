package proxy

import (
	"strings"
	"testing"
)

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
