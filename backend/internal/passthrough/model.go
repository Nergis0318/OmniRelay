package passthrough

import (
	"bytes"
	"encoding/json"
)

// extractModel reads the "model" field out of a buffered request-body prefix.
// It accepts a single JSON object, or newline-delimited / SSE-style objects so
// a preamble line that happens to carry a model never breaks parsing. Returns
// "" when the body is empty, not JSON, or carries no model field.
func extractModel(buf []byte) string {
	buf = bytes.TrimSpace(buf)
	if len(buf) == 0 {
		return ""
	}
	for _, raw := range bytes.Split(buf, []byte{'\n'}) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		if payload, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			line = bytes.TrimSpace(payload)
		}
		if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
			continue
		}
		var obj map[string]interface{}
		if json.Unmarshal(line, &obj) != nil {
			continue
		}
		if model, ok := obj["model"].(string); ok && model != "" {
			return model
		}
	}
	return ""
}
