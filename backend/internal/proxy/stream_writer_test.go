package proxy

import (
	"bytes"
	"testing"
)

func TestStreamWriterKeepAliveSkippedMidLine(t *testing.T) {
	var buf bytes.Buffer
	sw := newStreamWriter(&buf, &nopFlusher{})

	// Upstream chunk split mid-JSON: no trailing newline.
	sw.Write([]byte(`data: {"type":"response.content_part.done","item_id":"msg_1","output_index":1,`))
	sw.WriteKeepAlive()
	sw.Write([]byte(`"part":{}}` + "\n\n"))
	sw.WriteKeepAlive()

	got := buf.String()
	if bytes.Count([]byte(got), []byte(": keepalive")) != 1 {
		t.Fatalf("keepalive should only appear once (at line boundary); got %q", got)
	}
	if !bytes.HasSuffix([]byte(got), []byte(": keepalive\n\n")) {
		t.Fatalf("keepalive should be the last SSE comment; got %q", got)
	}
	for _, line := range bytes.Split([]byte(got), []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data: ")) {
			payload := bytes.TrimPrefix(line, []byte("data: "))
			if !bytes.Contains(payload, []byte(": keepalive")) {
				continue
			}
			t.Fatalf("keepalive spliced into data payload: %q", got)
		}
	}
}

func TestStreamWriterKeepAliveAtBoundary(t *testing.T) {
	var buf bytes.Buffer
	sw := newStreamWriter(&buf, &nopFlusher{})

	sw.Write([]byte("data: {\"a\":1}\n\n"))
	sw.WriteKeepAlive()

	got := buf.String()
	if got != "data: {\"a\":1}\n\n: keepalive\n\n" {
		t.Fatalf("got %q", got)
	}
}

type nopFlusher struct{}

func (n *nopFlusher) Flush() {}
