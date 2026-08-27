package passthrough

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// The capture watches the response bytes flowing through and picks up the
// usage the upstream reports about itself. Nothing is estimated and nothing is
// injected: a response carrying no usage fields yields no tokens.

const (
	maxJSONCaptureBytes = 4 << 20 // stop accumulating past this; relaying continues
	maxSSELineLength    = 1 << 20 // drop lines longer than this until the next newline
)

type captureMode int

const (
	modeNone captureMode = iota
	modeSSE
	modeJSON
)

// captureModeFor picks what to look for based on the upstream Content-Type.
func captureModeFor(contentType string) captureMode {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "text/event-stream"):
		return modeSSE
	case strings.Contains(ct, "application/json"), strings.Contains(ct, "x-ndjson"):
		return modeJSON
	default:
		return modeNone
	}
}

type tokenTotals struct {
	input          int64
	output         int64
	cacheWrite5m   int64
	cacheWrite1h   int64
	cacheRead      int64
	sawUsageSignal bool
}

// tokenCapture wraps the upstream body, observes every byte that flows through
// and hands them back untouched.
type tokenCapture struct {
	src     io.Reader
	mode    captureMode
	totals  tokenTotals
	lineBuf []byte // SSE: partial line carried across reads
	jsonBuf []byte // JSON/NDJSON: accumulated document (bounded)
	overCap bool   // JSON buffer hit its cap; accumulate nothing more
	dropRes bool   // SSE: dropping a line that exceeded maxSSELineLength
	done    bool   // source returned EOF/error; totals finalized
}

func newTokenCapture(src io.Reader, mode captureMode) *tokenCapture {
	return &tokenCapture{src: src, mode: mode}
}

// Close satisfies io.ReadCloser; the source body is closed by its owner.
func (t *tokenCapture) Close() error {
	if t == nil {
		return nil
	}
	if !t.done {
		t.finalize()
		t.done = true
	}
	if closer, ok := t.src.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Read passes bytes through from the source after feeding copies to the
// observer.
func (t *tokenCapture) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 && !t.done {
		t.observe(p[:n])
	}
	if err != nil {
		t.finalize()
		t.done = true
	}
	return n, err
}

// result reports the captured usage, or five nils when nothing was recognized.
// Safe on a nil capture.
func (t *tokenCapture) result() (input, output, cacheWrite5m, cacheWrite1h, cacheRead *int64) {
	if t == nil {
		return nil, nil, nil, nil, nil
	}
	if !t.done {
		t.finalize()
		t.done = true
	}
	tt := &t.totals
	if !tt.sawUsageSignal {
		return nil, nil, nil, nil, nil
	}
	ptr := func(v int64) *int64 { c := v; return &c }
	return ptr(tt.input), ptr(tt.output), ptr(tt.cacheWrite5m), ptr(tt.cacheWrite1h), ptr(tt.cacheRead)
}

func (t *tokenCapture) finalize() {
	if t.mode == modeJSON && len(t.jsonBuf) > 0 {
		doc := t.jsonBuf
		t.jsonBuf = nil
		t.observeJSONDocument(doc)
	}
}

func (t *tokenCapture) observe(chunk []byte) {
	switch t.mode {
	case modeSSE:
		t.observeSSE(chunk)
	case modeJSON:
		t.observeJSONChunk(chunk)
	}
}

// observeSSE assembles data lines across chunk boundaries ("data: {...}\n\n").
func (t *tokenCapture) observeSSE(chunk []byte) {
	for len(chunk) > 0 {
		idx := bytes.IndexByte(chunk, '\n')
		if idx < 0 {
			if !t.dropRes {
				t.lineBuf = append(t.lineBuf, chunk...)
				if len(t.lineBuf) > maxSSELineLength {
					t.dropRes = true
					t.lineBuf = nil
				}
			}
			return
		}
		line := chunk[:idx]
		chunk = chunk[idx+1:]

		dropping := t.dropRes
		t.dropRes = false

		if dropping || len(line) > maxSSELineLength {
			continue
		}

		if len(t.lineBuf) > 0 {
			line = append(t.lineBuf, line...)
			t.lineBuf = t.lineBuf[:0]
		}
		t.observeSSELine(string(line))
	}
}

func (t *tokenCapture) observeSSELine(line string) {
	payload, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "data:")
	if !ok {
		return
	}
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "[DONE]" {
		return
	}
	if !strings.Contains(payload, "usage") { // shared by every known usage field name
		return
	}
	var chunk map[string]interface{}
	if json.Unmarshal([]byte(payload), &chunk) != nil {
		return
	}
	t.extractFromObject(chunk)
}

func (t *tokenCapture) observeJSONChunk(chunk []byte) {
	if t.overCap {
		return
	}
	if len(t.jsonBuf)+len(chunk) > maxJSONCaptureBytes {
		t.overCap = true
		t.jsonBuf = nil
		return
	}
	t.jsonBuf = append(t.jsonBuf, chunk...)
}

// observeJSONDocument parses one whole JSON object, falling back to
// newline-delimited objects.
func (t *tokenCapture) observeJSONDocument(doc []byte) {
	doc = bytes.TrimSpace(doc)
	if len(doc) == 0 {
		return
	}
	var obj map[string]interface{}
	if json.Unmarshal(doc, &obj) == nil {
		t.extractFromObject(obj)
		return
	}
	for _, raw := range bytes.Split(doc, []byte{'\n'}) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		var line map[string]interface{}
		if json.Unmarshal(raw, &line) == nil {
			t.extractFromObject(line)
		}
	}
}

// extractFromObject pulls usage out of any known response shape, keyed by
// structure rather than provider type: Gemini puts usageMetadata at top level,
// OpenAI-compatible and Anthropic both nest under "usage" but spell their
// token fields differently, Anthropic SSE wraps them in message_start /
// message_delta events, and Ollama's native JSON names them prompt_eval_count
// / eval_count alongside a done flag.
func (t *tokenCapture) extractFromObject(obj map[string]interface{}) {
	if um, ok := obj["usageMetadata"].(map[string]interface{}); ok {
		t.totals.input = toInt64(um["promptTokenCount"])
		t.totals.output = toInt64(um["candidatesTokenCount"])
		t.totals.cacheRead = toInt64(um["cachedContentTokenCount"])
		t.totals.sawUsageSignal = true
	}

	if v, ok := obj["prompt_eval_count"]; ok {
		t.totals.input = toInt64(v)
		t.totals.sawUsageSignal = true
	}
	if v, ok := obj["eval_count"]; ok {
		t.totals.output = toInt64(v)
		t.totals.sawUsageSignal = true
	}

	if eventType, _ := obj["type"].(string); eventType == "message_start" {
		if msg, ok := obj["message"].(map[string]interface{}); ok {
			if usage, ok := msg["usage"].(map[string]interface{}); ok {
				t.applyTokenFields(usage)
			}
		}
	} else if eventType == "message_delta" {
		if usage, ok := obj["usage"].(map[string]interface{}); ok {
			if out, present := usage["output_tokens"]; present {
				t.totals.output = toInt64(out)
				t.totals.sawUsageSignal = true
			}
			t.applyTokenFields(usage) // late caches land here too
		}
	} else if usage, ok := obj["usage"].(map[string]interface{}); ok {
		t.applyTokenFields(usage)
	}
}

// applyTokenFields mirrors proxy/cost.go + proxy/cache.go: OpenAI-compatible
// providers spell them prompt_tokens/completion_tokens, Anthropic spells them
// input_tokens/output_tokens, and the cache keys differ per provider. All
// known keys are checked regardless of shape since only real fields match.
func (t *tokenCapture) applyTokenFields(usage map[string]interface{}) {
	if _, ok := usage["prompt_tokens"]; ok {
		t.totals.input = toInt64(usage["prompt_tokens"])
		t.totals.output = toInt64(usage["completion_tokens"])
	} else if _, ok := usage["input_tokens"]; ok {
		t.totals.input = toInt64(usage["input_tokens"])
		if out, present := usage["output_tokens"]; present {
			t.totals.output = toInt64(out)
		}
	}
	if v, ok := usage["cache_creation_input_tokens"]; ok {
		t.totals.cacheWrite5m = toInt64(v)
	}
	if v, ok := usage["cache_creation_extended_input_tokens"]; ok {
		t.totals.cacheWrite1h = toInt64(v)
	}
	if v, ok := usage["cache_read_input_tokens"]; ok {
		t.totals.cacheRead = toInt64(v)
	}
	if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
		if v, present := details["cached_tokens"]; present {
			t.totals.cacheRead = toInt64(v)
		}
	}
	t.totals.sawUsageSignal = true
}

// toInt64 accepts the number shapes encoding/json produces plus json.Number.
func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
