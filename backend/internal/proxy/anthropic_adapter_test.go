package proxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnthropicParseStreamChunkConvertsToOpenAIChunks(t *testing.T) {
	adapter := &AnthropicAdapter{}
	state := make(map[string]interface{})

	// A realistic three-event Anthropic Messages SSE sequence.
	chunk := `data: {"type":"message_start","message":{"usage":{"input_tokens":11}}}` + "\n\n" +
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}` + "\n\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":4}}` + "\n\n"

	got, inputTokens, outputTokens, err := adapter.ParseStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("ParseStreamChunk: %v", err)
	}
	if inputTokens != 11 {
		t.Errorf("inputTokens = %d, want 11", inputTokens)
	}
	if outputTokens != 4 {
		t.Errorf("outputTokens = %d, want 4", outputTokens)
	}

	text := string(got)
	// Each emitted SSE record must declare the OpenAI chunk envelope.
	if strings.Count(text, `"object":"chat.completion.chunk"`) != 3 {
		t.Errorf("expected 3 chat.completion.chunk records, got:\n%s", text)
	}
	// The message_start input_tokens should yield an assistant-role delta.
	if !strings.Contains(text, `"role":"assistant"`) {
		t.Errorf("missing assistant-role delta from message_start in:\n%s", text)
	}
	// The text_delta should appear as choices[0].delta.content.
	if !strings.Contains(text, `"content":"hello"`) {
		t.Errorf("missing text_delta content in:\n%s", text)
	}
	// The message_delta should produce a finish_reason and a usage block.
	if !strings.Contains(text, `"finish_reason":"stop"`) {
		t.Errorf("missing finish_reason=stop in:\n%s", text)
	}
	if !strings.Contains(text, `"prompt_tokens":11`) {
		t.Errorf("missing prompt_tokens=11 (carried from message_start) in:\n%s", text)
	}
	if !strings.Contains(text, `"total_tokens":15`) {
		t.Errorf("missing total_tokens=15 in:\n%s", text)
	}
}

func TestAnthropicParseStreamChunkIgnoresUnknownDeltaTypes(t *testing.T) {
	adapter := &AnthropicAdapter{}
	state := make(map[string]interface{})
	// input_json_delta and tool_use deltas should not produce content chunks.
	chunk := `data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"x\":1}"}}` + "\n\n"
	got, _, _, err := adapter.ParseStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("ParseStreamChunk: %v", err)
	}
	text := string(got)
	// Should emit only the empty placeholder chunk (no content).
	if strings.Contains(text, "\"content\"") {
		t.Errorf("unknown delta type should not emit content, got:\n%s", text)
	}
	if !strings.Contains(text, `"chat.completion.chunk"`) {
		t.Errorf("expected empty placeholder chunk, got:\n%s", text)
	}
}

func TestAnthropicParseStreamChunkEmitsPlaceholderForEmptyInput(t *testing.T) {
	adapter := &AnthropicAdapter{}
	state := make(map[string]interface{})
	got, inputTokens, outputTokens, err := adapter.ParseStreamChunk([]byte(""), state)
	if err != nil {
		t.Fatalf("ParseStreamChunk: %v", err)
	}
	if inputTokens != 0 || outputTokens != 0 {
		t.Errorf("empty input should produce zero tokens, got (%d, %d)", inputTokens, outputTokens)
	}
	text := string(got)
	if !strings.HasPrefix(text, "data: {") || !strings.HasSuffix(text, "\n\n") {
		t.Errorf("placeholder chunk should be a complete SSE record, got %q", text)
	}
	if !strings.Contains(text, `"chat.completion.chunk"`) {
		t.Errorf("placeholder chunk should declare chat.completion.chunk, got %q", text)
	}
}

func TestAnthropicParseStreamChunkSkipsDoneSentinel(t *testing.T) {
	adapter := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := "data: [DONE]\n\n"
	got, _, _, err := adapter.ParseStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("ParseStreamChunk: %v", err)
	}
	// With no real events, the adapter falls back to a single placeholder chunk.
	if strings.Count(string(got), `"chat.completion.chunk"`) != 1 {
		t.Errorf("expected exactly one placeholder chunk for [DONE]-only input, got:\n%s", got)
	}
}

func TestParseMessagesStreamChunkInterruptionTextDelta(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + interruptionMsg + "\"}}\n\n")
	out, _, _, err := a.ParseMessagesStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != nil {
		t.Errorf("out = %q, want nil (chunk dropped)", out)
	}
	if msg, _ := state["upstream_error"].(string); !isInterruptionText(msg) {
		t.Errorf("state upstream_error = %q", msg)
	}
}

func TestParseMessagesStreamChunkInterruptionErrorEvent(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"error\",\"error\":{\"type\":\"interruption\",\"message\":\"" + interruptionMsg + "\"}}\n\n")
	out, _, _, err := a.ParseMessagesStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != nil {
		t.Errorf("out = %q, want nil", out)
	}
	if msg, _ := state["upstream_error"].(string); !isInterruptionText(msg) {
		t.Errorf("state upstream_error = %q", msg)
	}
}

func TestParseMessagesStreamChunkInterruptionErrorTypeOnly(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"error\",\"error\":{\"type\":\"interruption\",\"message\":\"some other error\"}}\n\n")
	out, _, _, err := a.ParseMessagesStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != nil {
		t.Errorf("out = %q, want nil", out)
	}
	if msg, _ := state["upstream_error"].(string); msg == "" {
		t.Error("state upstream_error not set for error.type=interruption")
	}
}

func TestParseMessagesStreamChunkOtherErrorEventPassesThrough(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"rate limited\"}}\n\n")
	out, _, _, err := a.ParseMessagesStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out == nil {
		t.Error("out = nil, want passthrough for non-interruption errors")
	}
	if _, ok := state["upstream_error"]; ok {
		t.Error("upstream_error set for non-interruption error")
	}
}

func TestParseStreamChunkInterruptionTextDelta(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + interruptionMsg + "\"}}\n\n")
	out, _, _, err := a.ParseStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != nil {
		t.Errorf("out = %q, want nil (dropped)", out)
	}
	if msg, _ := state["upstream_error"].(string); !isInterruptionText(msg) {
		t.Errorf("state upstream_error = %q", msg)
	}
}

func TestParseStreamChunkInterruptionErrorEvent(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"error\",\"error\":{\"type\":\"interruption\",\"message\":\"" + interruptionMsg + "\"}}\n\n")
	out, _, _, err := a.ParseStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != nil {
		t.Errorf("out = %q, want nil", out)
	}
	if msg, _ := state["upstream_error"].(string); !isInterruptionText(msg) {
		t.Errorf("state upstream_error = %q", msg)
	}
}

func TestParseStreamChunkInterruptionErrorTypeOnly(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"error\",\"error\":{\"type\":\"interruption\",\"message\":\"some other error\"}}\n\n")
	out, _, _, err := a.ParseStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != nil {
		t.Errorf("out = %q, want nil", out)
	}
	if msg, _ := state["upstream_error"].(string); msg == "" {
		t.Error("state upstream_error not set for error.type=interruption")
	}
}

func TestParseStreamChunkNormalDeltaUnchanged(t *testing.T) {
	a := &AnthropicAdapter{}
	state := make(map[string]interface{})
	chunk := []byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
	out, _, _, err := a.ParseStreamChunk(chunk, state)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Contains(out, []byte(`"content":"hi"`)) {
		t.Errorf("out = %s, want content delta", out)
	}
	if _, ok := state["upstream_error"]; ok {
		t.Error("upstream_error set for normal delta")
	}
}
