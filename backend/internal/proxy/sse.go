package proxy

import (
	"bufio"
	"encoding/json"
	"strings"
)

// forEachSSELine calls fn for every `data: ...` line in the SSE chunk.
// fn receives the raw JSON payload as json.RawMessage. A nil payload signals [DONE].
// If fn returns true, iteration stops early.
func forEachSSELine(data []byte, fn func(raw json.RawMessage) bool) {
	text := strings.TrimSpace(string(data))
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			if fn(nil) {
				return
			}
			continue
		}
		if fn(json.RawMessage(payload)) {
			return
		}
	}
}
