// Package passthrough relays requests that carry the whole upstream URL inside
// the request path, e.g. /https://api.openai.com/v1/chat/completions.
//
// Unlike the provider-routed paths, nothing is translated here: the method,
// headers, query string and body bytes go upstream as they arrived, and the
// upstream response is streamed back untouched. No provider lookup or model
// resolution happens; performance is always measured into its own table, and
// usage the upstream itself reports in its response (usage fields, SSE events)
// is picked up along the way - no tokens are estimated and none are injected.
package passthrough

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"omnirelay/internal/models"
)

// Header names carrying the measured timings back to the caller.
const (
	HeaderRelay      = "X-Omni-Relay"
	HeaderDNSMs      = "X-Omni-Dns-Ms"
	HeaderConnectMs  = "X-Omni-Connect-Ms"
	HeaderTLSMs      = "X-Omni-Tls-Ms"
	HeaderTTFBMs     = "X-Omni-Ttfb-Ms"
	HeaderTargetHost = "X-Omni-Target"
)

const copyBufferSize = 32 << 10

// Options configures a Relay.
type Options struct {
	// AllowPrivate disables the SSRF guard, for relaying to local/self-hosted
	// models such as Ollama or LM Studio.
	AllowPrivate bool
	// Timeout caps the whole exchange, including reading a streamed body.
	Timeout time.Duration
	// DialTimeout caps connection setup. Zero means 15s.
	DialTimeout time.Duration
}

// Relay forwards URL-embedded requests upstream and measures them. Requests
// whose path is not a passthrough URL are handed to next unchanged.
type Relay struct {
	opts   Options
	sink   func(models.PassthroughLog)
	next   http.Handler
	client *http.Client
}

// New builds a Relay wrapping next. sink receives one record per relayed
// request and must never block, since it is called on the measured path.
func New(opts Options, sink func(models.PassthroughLog), next http.Handler) *Relay {
	dialTimeout := opts.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 15 * time.Second
	}
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}

	transport := &http.Transport{
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   dialTimeout,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
		DialContext:           dialer.DialContext,
	}
	if !opts.AllowPrivate {
		transport.DialContext = guardedDial(dialer)
	}

	return &Relay{
		opts: opts,
		sink: sink,
		next: next,
		client: &http.Client{
			Transport: transport,
			Timeout:   opts.Timeout,
			// A relay reports what upstream answered, redirects included.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// guardedDial resolves the host itself so every candidate address is checked
// against the blocklist right before dialing, which also closes the
// DNS-rebinding window between validation and connection.
func guardedDial(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %v", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses for %s", host)
		}
		for _, ip := range ips {
			if BlockedIP(ip.IP) {
				return nil, fmt.Errorf("%s resolves to blocked address %s", host, ip.IP)
			}
		}
		var lastErr error
		for _, ip := range ips {
			conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if derr == nil {
				return conn, nil
			}
			lastErr = derr
		}
		return nil, lastErr
	}
}

func (rl *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !IsPassthroughPath(req.URL.Path) {
		rl.next.ServeHTTP(w, req)
		return
	}
	rl.relay(w, req)
}

func (rl *Relay) relay(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("passthrough relay panic: %v", rec)
			writeError(w, http.StatusBadGateway, "passthrough relay failed")
		}
	}()

	startedAt := time.Now()
	record := models.PassthroughLog{Method: req.Method, Path: req.URL.Path, StartedAt: startedAt}

	target, err := ParseTarget(req.URL.Path)
	if err != nil {
		record.StatusCode = http.StatusBadRequest
		record.IsError = true
		record.ErrMessage = err.Error()
		record.TotalMs = time.Since(startedAt).Milliseconds()
		rl.emit(record)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target.RawQuery = mergeQuery(target.RawQuery, req.URL.RawQuery)
	record.Host = target.Host
	record.Path = target.EscapedPath()

	timings := &timing{start: startedAt}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), timings.trace()))

	upReq, sent, err := newUpstreamRequest(req, target.String())
	if err != nil {
		record.StatusCode = http.StatusBadRequest
		rl.fail(w, record, nil, timings, startedAt, err)
		return
	}
	copyForwardHeaders(upReq.Header, req.Header)

	resp, err := rl.client.Do(upReq)
	if err != nil {
		rl.fail(w, record, sent, timings, startedAt, err)
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set(HeaderRelay, "passthrough")
	w.Header().Set(HeaderTargetHost, target.Host)
	timings.setHeaders(w.Header())
	w.WriteHeader(resp.StatusCode)

	var capture *tokenCapture
	if mode := captureModeFor(resp.Header.Get("Content-Type")); mode != modeNone {
		capture = newTokenCapture(resp.Body, mode)
		resp.Body = capture
	}

	written, _ := rl.stream(w, resp.Body, timings)

	record.StatusCode = resp.StatusCode
	record.IsError = resp.StatusCode >= http.StatusBadRequest
	record.DNSMs = timings.dns()
	record.ConnectMs = timings.connect()
	record.TLSMs = timings.tls()
	record.TTFBMs = timings.ttfb()
	record.TTFTMs = timings.ttft()
	record.TotalMs = time.Since(startedAt).Milliseconds()
	record.RequestBytes = sent.bytes()
	record.ResponseBytes = written
	record.InputTokens, record.OutputTokens,
		record.CacheWrite5MTokens, record.CacheWrite1HTokens,
		record.CacheReadTokens = capture.result()
	if record.IsError {
		record.ErrMessage = fmt.Sprintf("upstream returned %d", resp.StatusCode)
	}
	rl.emit(record)
}

// newUpstreamRequest clones the client request onto the embedded target URL,
// keeping the original body stream and its framing (Content-Length when the
// client sent one, chunked otherwise) so nothing is re-encoded.
func newUpstreamRequest(req *http.Request, upstreamURL string) (*http.Request, *countingReader, error) {
	var body io.Reader
	var sent *countingReader
	if req.Body != nil && req.Body != http.NoBody {
		sent = &countingReader{reader: req.Body}
		body = sent
	}

	upReq, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamURL, body)
	if err != nil {
		return nil, nil, err
	}
	if req.ContentLength >= 0 {
		upReq.ContentLength = req.ContentLength
	}
	return upReq, sent, nil
}

// countingReader tracks how many request-body bytes the transport pulled. The
// value is only meaningful once the exchange finished, which is when callers
// read it.
type countingReader struct {
	reader io.Reader
	n      atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c == nil {
		return 0, io.EOF
	}
	n, err := c.reader.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func (c *countingReader) bytes() int64 {
	if c == nil {
		return 0
	}
	return c.n.Load()
}

// stream copies the upstream body to the client chunk by chunk, flushing as it
// goes so server-sent events reach the caller the moment upstream emits them.
func (rl *Relay) stream(w http.ResponseWriter, body io.Reader, timings *timing) (int64, error) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, copyBufferSize)
	var written int64

	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			timings.markFirstByte()
			nw, werr := w.Write(buf[:n])
			written += int64(nw)
			if flusher != nil {
				flusher.Flush()
			}
			if werr != nil {
				return written, werr
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return written, nil
			}
			return written, rerr
		}
	}
}

// fail records and reports a relay that never produced an upstream response.
func (rl *Relay) fail(w http.ResponseWriter, record models.PassthroughLog, sent *countingReader, timings *timing, startedAt time.Time, cause error) {
	if record.StatusCode == 0 {
		record.StatusCode = http.StatusBadGateway
	}
	record.IsError = true
	record.ErrMessage = cause.Error()
	record.TTFBMs = timings.ttfb()
	record.TotalMs = time.Since(startedAt).Milliseconds()
	record.RequestBytes = sent.bytes()
	rl.emit(record)
	writeError(w, record.StatusCode, record.ErrMessage)
}

func (rl *Relay) emit(record models.PassthroughLog) {
	if rl.sink == nil {
		return
	}
	rl.sink(record)
}

// timing collects the per-phase durations of one relay. Callbacks run on the
// transport goroutine while the body copy runs on the handler goroutine, so
// every field is touched atomically.
type timing struct {
	start time.Time

	dnsStart     atomic.Int64
	dnsDone      atomic.Int64
	connectStart atomic.Int64
	connectDone  atomic.Int64
	tlsStart     atomic.Int64
	tlsDone      atomic.Int64
	ttfbOffset   atomic.Int64
	ttftOffset   atomic.Int64
}

func (t *timing) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { t.dnsStart.Store(time.Since(t.start).Microseconds()) },
		DNSDone:              func(httptrace.DNSDoneInfo) { t.dnsDone.Store(time.Since(t.start).Microseconds()) },
		ConnectStart:         func(string, string) { t.connectStart.Store(time.Since(t.start).Microseconds()) },
		ConnectDone:          func(string, string, error) { t.connectDone.Store(time.Since(t.start).Microseconds()) },
		TLSHandshakeStart:    func() { t.tlsStart.Store(time.Since(t.start).Microseconds()) },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.tlsDone.Store(time.Since(t.start).Microseconds()) },
		GotFirstResponseByte: func() { t.ttfbOffset.Store(time.Since(t.start).Microseconds()) },
	}
}

func (t *timing) markFirstByte() { t.ttftOffset.CompareAndSwap(0, time.Since(t.start).Microseconds()) }

func (t *timing) dns() *int64     { return phase(t.dnsStart.Load(), t.dnsDone.Load()) }
func (t *timing) connect() *int64 { return phase(t.connectStart.Load(), t.connectDone.Load()) }
func (t *timing) tls() *int64     { return phase(t.tlsStart.Load(), t.tlsDone.Load()) }
func (t *timing) ttfb() *int64    { return offset(t.ttfbOffset.Load()) }
func (t *timing) ttft() *int64    { return offset(t.ttftOffset.Load()) }

// phase converts a start/done microsecond pair into milliseconds, or nil when
// the phase never ran (a reused keep-alive connection, for instance).
func phase(start, done int64) *int64 {
	if start == 0 || done <= start {
		return nil
	}
	ms := (done - start) / 1000
	return &ms
}

// offset converts a start-relative microsecond stamp into milliseconds.
func offset(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	ms := value / 1000
	return &ms
}

func (t *timing) setHeaders(header http.Header) {
	setMsHeader(header, HeaderDNSMs, t.dns())
	setMsHeader(header, HeaderConnectMs, t.connect())
	setMsHeader(header, HeaderTLSMs, t.tls())
	setMsHeader(header, HeaderTTFBMs, t.ttfb())
}

func setMsHeader(header http.Header, name string, value *int64) {
	if value != nil {
		header.Set(name, strconv.FormatInt(*value, 10))
	}
}

// hopByHopHeaders are connection-scoped and must not cross a relay.
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Proxy-Connection":    {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// copyForwardHeaders forwards every client header verbatim - including
// Authorization and x-api-key, which are the caller's own upstream
// credentials - minus the connection-scoped hop-by-hop set.
func copyForwardHeaders(dst, src http.Header) {
	skip := connectionTokens(src)
	for name, values := range src {
		if _, drop := hopByHopHeaders[http.CanonicalHeaderKey(name)]; drop {
			continue
		}
		if _, drop := skip[name]; drop {
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

// connectionTokens lists headers named by the Connection header itself, which
// RFC 9112 also treats as connection-scoped.
func connectionTokens(src http.Header) map[string]struct{} {
	skip := make(map[string]struct{})
	for _, value := range src.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				skip[http.CanonicalHeaderKey(name)] = struct{}{}
			}
		}
	}
	return skip
}

func copyResponseHeaders(dst, src http.Header) {
	for name, values := range src {
		if _, drop := hopByHopHeaders[http.CanonicalHeaderKey(name)]; drop {
			continue
		}
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck // best-effort error body
		"error": map[string]interface{}{
			"message": message,
			"type":    "upstream_error",
			"param":   nil,
			"code":    status,
		},
	})
}
