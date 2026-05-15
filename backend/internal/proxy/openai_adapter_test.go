package proxy

import (
	"strings"
	"testing"
)

func TestOpenAIParseMessagesStreamChunkConvertsToAnthropicEvents(t *testing.T) {
	adapter := &OpenAIAdapter{}
	state := make(map[string]interface{})

	chunk := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\n"

	got, inputTokens, outputTokens, err := adapter.ParseMessagesStreamChunk([]byte(chunk), state)
	if err != nil {
		t.Fatalf("ParseMessagesStreamChunk returned error: %v", err)
	}

	text := string(got)
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"\"text\":\"hello\"",
		"event: content_block_stop",
		"event: message_delta",
		"\"stop_reason\":\"end_turn\"",
		"event: message_stop",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("converted stream missing %q in:\n%s", want, text)
		}
	}

	if inputTokens != 3 || outputTokens != 2 {
		t.Fatalf("tokens = (%d, %d), want (3, 2)", inputTokens, outputTokens)
	}
}

func TestOpenAIParseMessagesStreamChunkKeepsStateAcrossChunks(t *testing.T) {
	adapter := &OpenAIAdapter{}
	state := make(map[string]interface{})

	first, _, _, err := adapter.ParseMessagesStreamChunk([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"), state)
	if err != nil {
		t.Fatalf("first chunk returned error: %v", err)
	}
	second, _, _, err := adapter.ParseMessagesStreamChunk([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n"), state)
	if err != nil {
		t.Fatalf("second chunk returned error: %v", err)
	}

	if strings.Count(string(first)+string(second), "event: message_start") != 1 {
		t.Fatalf("message_start should only be emitted once; first=%q second=%q", first, second)
	}
}
