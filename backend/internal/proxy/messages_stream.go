package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// messagesStreamWriter buffers Anthropic Messages SSE events and tracks the
// shared streaming state (started / content_started / stopped, token totals)
// so adapters can focus on provider-specific delta extraction.
//
// State keys mirrored across chunks via the caller-supplied map:
//   - started, content_started, stopped (bool)
//   - input_tokens, output_tokens (int64)
type messagesStreamWriter struct {
	buf   bytes.Buffer
	state map[string]interface{}
}

func newMessagesStreamWriter(state map[string]interface{}) *messagesStreamWriter {
	return &messagesStreamWriter{state: state}
}

func (w *messagesStreamWriter) writeEvent(event map[string]interface{}) {
	jsonEvent, _ := json.Marshal(event)
	w.buf.WriteString("event: ")
	w.buf.WriteString(fmt.Sprint(event["type"]))
	w.buf.WriteString("\n")
	w.buf.WriteString("data: ")
	w.buf.Write(jsonEvent)
	w.buf.WriteString("\n\n")
}

// ensureStarted emits message_start + content_block_start exactly once each across all chunks for a stream.
func (w *messagesStreamWriter) ensureStarted() {
	if !boolState(w.state, "started") {
		w.writeEvent(map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":            fmt.Sprintf("msg_%d", time.Now().UnixNano()),
				"type":          "message",
				"role":          "assistant",
				"content":       []interface{}{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]interface{}{
					"input_tokens": int64State(w.state, "input_tokens", 0),
				},
			},
		})
		w.state["started"] = true
	}
	if !boolState(w.state, "content_started") {
		w.writeEvent(map[string]interface{}{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		})
		w.state["content_started"] = true
	}
}

// textDelta emits a content_block_delta of type "text_delta" with the provided text,
// after making sure the stream has been opened with message_start / content_block_start.
func (w *messagesStreamWriter) textDelta(text string) {
	if text == "" {
		return
	}
	w.ensureStarted()
	w.writeEvent(map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": text,
		},
	})
}

// finish emits content_block_stop + message_delta (with output_tokens) + message_stop
// exactly once across the lifetime of the stream.
func (w *messagesStreamWriter) finish(stopReason string, outputTokens int64) {
	if boolState(w.state, "stopped") {
		return
	}
	w.ensureStarted()
	w.writeEvent(map[string]interface{}{"type": "content_block_stop", "index": 0})
	w.writeEvent(map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"output_tokens": int64State(w.state, "output_tokens", outputTokens),
		},
	})
	w.writeEvent(map[string]interface{}{"type": "message_stop"})
	w.state["stopped"] = true
}

// bytes returns the buffered SSE payload, or nil if nothing was written for this chunk.
func (w *messagesStreamWriter) bytes() []byte {
	if w.buf.Len() == 0 {
		return nil
	}
	return w.buf.Bytes()
}
