package passthrough

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// feed drives a capture over arbitrary chunk boundaries, as the relay's copy
// loop would.
func feed(t *testing.T, mode captureMode, chunks []string) (input, output, cw5m, cw1h, cr *int64) {
	t.Helper()
	cap := newTokenCapture(&chunkReader{chunks: chunks}, mode)
	buf := make([]byte, 512)
	for {
		_, err := cap.Read(buf)
		if err != nil {
			break
		}
	}
	return cap.result()
}

// chunkReader serves each chunk fully, spread over however many reads the
// caller's buffer needs - like a real body does.
type chunkReader struct {
	chunks []string
	i      int
	cur    string
}

func (c *chunkReader) Read(p []byte) (int, error) {
	for {
		if c.cur != "" {
			n := copy(p, c.cur)
			c.cur = c.cur[n:]
			return n, nil
		}
		if c.i >= len(c.chunks) {
			return 0, io.EOF
		}
		c.cur = c.chunks[c.i]
		c.i++
	}
}

func TestCaptureModeFor(t *testing.T) {
	cases := map[string]captureMode{
		"text/event-stream":               modeSSE,
		"application/json":                modeJSON,
		"application/x-ndjson":            modeJSON,
		"application/json; charset=utf-8": modeJSON,
		"text/html; charset=utf-8":        modeNone,
		"":                                modeNone,
		"application/octet-stream":        modeNone,
		"TEXT/EVENT-STREAM":               modeSSE,
		"application/vnd.api+json":        modeNone, // not application/json
	}
	for ct, want := range cases {
		if got := captureModeFor(ct); got != want {
			t.Errorf("captureModeFor(%q) = %v, want %v", ct, got, want)
		}
	}
}

func TestCaptureOpenAISSE(t *testing.T) {
	chunks := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"llo\"}}]}\n\n",
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":45,\"prompt_tokens_details\":{\"cached_tokens\":100}}}\n\n",
		"data: [DONE]\n\n",
	}
	in, out, cw5m, cw1h, cr := feed(t, modeSSE, chunks)
	if in == nil || *in != 120 {
		t.Errorf("input = %v, want 120", in)
	}
	if out == nil || *out != 45 {
		t.Errorf("output = %v, want 45", out)
	}
	if cr == nil || *cr != 100 {
		t.Errorf("cache read = %v, want 100", cr)
	}
	// A recognized usage object records all five columns; fields the provider
	// does not report are 0, not null.
	if cw5m == nil || *cw5m != 0 || cw1h == nil || *cw1h != 0 {
		t.Errorf("cache write should be recorded as 0 for openai, got %v/%v", cw5m, cw1h)
	}
}

func TestCaptureSSEWithoutUsage(t *testing.T) {
	chunks := []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
	in, out, cw5m, cw1h, cr := feed(t, modeSSE, chunks)
	if in != nil || out != nil || cw5m != nil || cw1h != nil || cr != nil {
		t.Errorf("no usage chunk reported tokens: %v %v %v %v %v", in, out, cw5m, cw1h, cr)
	}
}

func TestCaptureSplitAcrossChunks(t *testing.T) {
	raw := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":77,\"completion_tokens\":33}}\n\n"
	half := len(raw) / 2
	in, out, _, _, _ := feed(t, modeSSE, []string{raw[:half], raw[half:]})
	if in == nil || *in != 77 || out == nil || *out != 33 {
		t.Errorf("split usage lost: input=%v output=%v", in, out)
	}
}

func TestCaptureAnthropicSSE(t *testing.T) {
	chunks := []string{
		`event: message_start`,
		"\n",
		`data: {"type":"message_start","message":{"usage":{"input_tokens":210,"cache_creation_input_tokens":88,"cache_read_input_tokens":12}}}` + "\n\n",
		`data: {"type":"content_block_delta","delta":{"text":"x"}}` + "\n\n",
		`data: {"type":"message_delta","usage":{"output_tokens":64}}` + "\n\n",
	}
	in, out, cw5m, cw1h, cr := feed(t, modeSSE, chunks)
	if in == nil || *in != 210 {
		t.Errorf("input = %v, want 210", in)
	}
	if out == nil || *out != 64 {
		t.Errorf("output = %v, want 64", out)
	}
	if cw5m == nil || *cw5m != 88 {
		t.Errorf("cache write 5m = %v, want 88", cw5m)
	}
	if cr == nil || *cr != 12 {
		t.Errorf("cache read = %v, want 12", cr)
	}
	if cw1h == nil || *cw1h != 0 {
		t.Errorf("cache write 1h = %v, want recorded 0", cw1h)
	}
}

func TestCaptureGeminiSSELastWins(t *testing.T) {
	chunks := []string{
		`data: {"candidates":[],"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":5}}` + "\n\n",
		`data: {"candidates":[],"usageMetadata":{"promptTokenCount":500,"candidatesTokenCount":55,"cachedContentTokenCount":40}}` + "\n\n",
	}
	in, out, _, _, cr := feed(t, modeSSE, chunks)
	if in == nil || *in != 500 {
		t.Errorf("input = %v, want 500 (cumulative last-wins)", in)
	}
	if out == nil || *out != 55 {
		t.Errorf("output = %v, want 55", out)
	}
	if cr == nil || *cr != 40 {
		t.Errorf("cache read = %v, want 40", cr)
	}
}

func TestCaptureOllamaNDJSON(t *testing.T) {
	doc := "{\"model\":\"llama3\",\"done\":false}\n{\"model\":\"llama3\",\"done\":true,\"prompt_eval_count\":31,\"eval_count\":17}\n"
	in, out, _, _, _ := feed(t, modeJSON, []string{doc})
	if in == nil || *in != 31 {
		t.Errorf("input = %v, want 31", in)
	}
	if out == nil || *out != 17 {
		t.Errorf("output = %v, want 17", out)
	}
}

func TestCaptureNonStreamingJSON(t *testing.T) {
	openai := `{"id":"x","usage":{"prompt_tokens":9,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`
	anthropic := `{"id":"msg_1","usage":{"input_tokens":21,"output_tokens":7,"cache_creation_input_tokens":5,"cache_creation_extended_input_tokens":2,"cache_read_input_tokens":11}}`
	gemini := `{"candidates":[],"usageMetadata":{"promptTokenCount":300,"candidatesTokenCount":30,"cachedContentTokenCount":25}}`

	if in, out, _, _, cr := feed(t, modeJSON, []string{openai}); in == nil || *in != 9 || *out != 4 || *cr != 3 {
		t.Errorf("openai body: %v %v %v", in, out, cr)
	}
	if in, out, cw5m, cw1h, cr := feed(t, modeJSON, []string{anthropic}); in == nil || *in != 21 || *out != 7 || *cw5m != 5 || *cw1h != 2 || *cr != 11 {
		t.Errorf("anthropic body: %v %v %v %v %v", in, out, cw5m, cw1h, cr)
	}
	if in, out, _, _, cr := feed(t, modeJSON, []string{gemini}); in == nil || *in != 300 || *out != 30 || *cr != 25 {
		t.Errorf("gemini body: %v %v %v", in, out, cr)
	}
}

func TestCaptureIgnoresUnrelatedJSON(t *testing.T) {
	htmlish := `{"error":{"message":"rate limited","code":429}}`
	in, out, cw5m, cw1h, cr := feed(t, modeJSON, []string{htmlish})
	if in != nil || out != nil || cw5m != nil || cw1h != nil || cr != nil {
		t.Errorf("unrelated body produced tokens: %v %v %v %v %v", in, out, cw5m, cw1h, cr)
	}
}

func TestCaptureJSONOverflow(t *testing.T) {
	cap := newTokenCapture(
		bytes.NewReader([]byte(`{"usage":{"prompt_tokens":1},"pad":"`+strings.Repeat("x", maxJSONCaptureBytes)+`"}}`)),
		modeJSON,
	)
	buf := make([]byte, 4096)
	total := 0
	for {
		n, err := cap.Read(buf)
		total += n
		if err != nil {
			break
		}
	}
	if !cap.overCap {
		t.Error("expected the JSON buffer to hit its cap")
	}
	if total < maxJSONCaptureBytes {
		t.Errorf("only %d bytes were read through; relaying must not be truncated", total)
	}
	if in, _, _, _, _ := cap.result(); in == nil {
		// usage arrived before the padding overflowed the buffer and stays valid.
	} else if *in != 1 {
		t.Errorf("input = %d, want 1", *in)
	}
}

func TestCaptureLongSSLineDroppedButRelayContinues(t *testing.T) {
	long := strings.Repeat("a", maxSSELineLength+10)
	chunks := []string{
		"data: " + long + "\n",
		`data: {"usage":{"prompt_tokens":8,"completion_tokens":2}}` + "\n\n",
	}
	in, out, _, _, _ := feed(t, modeSSE, chunks)
	if in == nil || *in != 8 {
		t.Errorf("input = %v, want 8 after dropping an oversized line", in)
	}
	if out == nil || *out != 2 {
		t.Errorf("output = %v, want 2", out)
	}
}

func TestCaptureCRNotLeakingBetweenLines(t *testing.T) {
	// Windows-style CRLF endings must not corrupt the payload prefixes.
	chunks := []string{
		"data: {\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":1}}\r\n\r\n",
	}
	in, _, _, _, _ := feed(t, modeSSE, chunks)
	if in == nil || *in != 6 {
		t.Errorf("input = %v, want 6", in)
	}
}

func TestResultOnNilCapture(t *testing.T) {
	var cap *tokenCapture
	if in, out, cw5m, cw1h, cr := cap.result(); in != nil || out != nil || cw5m != nil || cw1h != nil || cr != nil {
		t.Error("nil capture must report five nils")
	}
}
