# "Empty message" → 표준 503 변환 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 업스트림(conduit 릴레이 등)이 `[Empty message]`를 콘텐츠로 전달하는 것을 모든 스트림 경로에서 감지해 HTTP 503 retryable 에러(`server_error`/`overloaded_error`)로 변환한다.

**Architecture:** 기존 interruption(503) 처리 메커니즘(`writeStreamUpstreamError` / `abortErrorContent`)의 "Empty message" 분기를 200 `api_error`에서 503으로 전환한다. Responses-API 형식 스트림(conduit 케이스, `"[Empty"`+`" message]"` 분할 도착)은 `handleStreamResponse`/`handleRawStreamResponse`에서 형식 감지 후 전체 버퍼링 → 종료 시 누적 텍스트 검사로 503 전환, 정상이면 버퍼 재생.

**Tech Stack:** Go 1.25, Gin, modernc.org/sqlite (테스트용), 기존 proxy 패키지 테스트 패턴 (httptest + gin.CreateTestContext)

## Global Constraints

- 스펙: `docs/superpowers/specs/2026-08-06-empty-message-503-design.md`
- 에러 형태: Anthropic 형식 → 503 + `overloaded_error`, OpenAI 형식 → 503 + `server_error` (interruption과 동일, `writeStreamUpstreamError` 분기 재사용)
- 감지 텍스트: `"Empty message"` **및** `"[Empty message]"` (브라켓 포함 — 실제 conduit 캡처 텍스트가 `[Empty message]`임)
- "Request failed."(502 `api_error`) 동작 변경 금지
- Responses 형식 판정 트리거: 청크 바이트에 `"type":"response.` 서브스트링
- 텍스트 누적: `response.output_text.delta`의 `delta` + `response.output_text.done`의 `text` + `response.completed`의 `response.output_text` (finalText가 있으면 우선)
- 검증: `go test ./...`, `go vet ./...` (backend/ 에서 실행)
- 커밋 메시지: conventional commits (repo 스타일)

---

### Task 1: "Empty message" 감지 헬퍼 확장 + 스트림 에러를 503으로 전환

**Files:**
- Modify: `backend/internal/proxy/proxy_helpers.go:26-28` (`isUpstreamErrorContent`)
- Modify: `backend/internal/proxy/stream.go:83-112` (`writeStreamUpstreamError`)
- Test: `backend/internal/proxy/proxy_helpers_test.go` (신규 테스트 추가)
- Test: `backend/internal/proxy/interruption_test.go:272-310`, `:312-353` (기존 테스트 수정)

**Interfaces:**
- Consumes: 기존 `isUpstreamErrorContent(text string) bool`, `isInterruptionText(text string) bool`
- Produces: `isUpstreamErrorContent`가 `"Empty message"`와 `"[Empty message]"` 모두 매치. `writeStreamUpstreamError(c, errMsg)`가 "Empty message"도 503으로 응답 (이후 Task 3/4가 의존)

- [ ] **Step 1: `isUpstreamErrorContent` 확장 (브라켓 변형 매치)**

```go
// isUpstreamErrorContent checks if the response text is a known error sent by
// an upstream that does not follow standard error reporting. Both the plain
// and bracketed forms occur in the wild.
func isUpstreamErrorContent(text string) bool {
	text = strings.TrimSpace(text)
	return text == "Empty message" || text == "[Empty message]"
}
```

- [ ] **Step 2: `writeStreamUpstreamError`에서 "Empty message" → 503 병합**

`stream.go`의 현재 구조:

```go
	if isInterruptionText(errMsg) {
		errType := "overloaded_error"
		if errFmt != apiresponse.FormatAnthropic {
			errType = "server_error"
		}
		c.Status(http.StatusServiceUnavailable)
		c.Header("Content-Type", "application/json")
		c.Writer.Write(reformatError(upstreamError{ErrType: errType, Message: errMsg, Code: "upstream_error"}, errFmt, requestID))
		return
	}
	// Legacy "Empty message" behavior: 200 api_error, matching the
	// non-streaming path (abortErrorContent).
	if isUpstreamErrorContent(errMsg) {
		c.Status(http.StatusOK)
		c.Header("Content-Type", "application/json")
		c.Writer.Write(reformatError(upstreamError{ErrType: "api_error", Message: errMsg}, errFmt, requestID))
		return
	}
	c.Status(http.StatusBadGateway)
```

다음으로 교체 (interruption 분기 조건만 확장하고 200 분기 제거):

```go
	// Interruptions and "Empty message" are temporary upstream failures →
	// 503 retryable (server_error / overloaded_error).
	if isInterruptionText(errMsg) || isUpstreamErrorContent(errMsg) {
		errType := "overloaded_error"
		if errFmt != apiresponse.FormatAnthropic {
			errType = "server_error"
		}
		c.Status(http.StatusServiceUnavailable)
		c.Header("Content-Type", "application/json")
		c.Writer.Write(reformatError(upstreamError{ErrType: errType, Message: errMsg, Code: "upstream_error"}, errFmt, requestID))
		return
	}
	c.Status(http.StatusBadGateway)
```

- [ ] **Step 3: 헬퍼 단위 테스트 추가** (`proxy_helpers_test.go` 끝에 추가)

```go
func TestIsUpstreamErrorContent(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Empty message", true},
		{"[Empty message]", true},
		{"  Empty message  ", true},
		{"normal reply", false},
		{"Temporary service interruption. Retry.", false},
	}
	for _, tc := range cases {
		if got := isUpstreamErrorContent(tc.in); got != tc.want {
			t.Errorf("isUpstreamErrorContent(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 4: 기존 스트림 테스트 2개를 503 기대값으로 수정** (`interruption_test.go`)

`TestHandleStreamResponseEmptyMessageStill200` (현재 200/`api_error` 기대)를 `TestHandleStreamResponseEmptyMessage503`로 개명하고 기대값 변경:

```go
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "data: ") {
		t.Errorf("stream data leaked into error body: %s", w.Body.String())
	}
```

`TestHandleMessagesStreamResponseEmptyMessageStill200` → `TestHandleMessagesStreamResponseEmptyMessage503` (경로 `/v1/messages` → Anthropic 형식):

```go
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"overloaded_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "data: ") {
		t.Errorf("stream data leaked into error body: %s", w.Body.String())
	}
```

- [ ] **Step 5: 테스트 실행**

Run: `go test ./internal/proxy/ -run 'EmptyMessage|IsUpstreamErrorContent' -v` (backend/ 에서)
Expected: 신규 테스트 + 수정된 테스트 모두 PASS

- [ ] **Step 6: 커밋**

```bash
git add backend/internal/proxy/proxy_helpers.go backend/internal/proxy/proxy_helpers_test.go backend/internal/proxy/stream.go backend/internal/proxy/interruption_test.go
git commit -m "feat: convert upstream Empty message to 503 in streams"
```

---

### Task 2: 비스트리밍 "Empty message" → 503 일관화

**Files:**
- Modify: `backend/internal/proxy/proxy_helpers.go:49-56` (`abortErrorContent`)
- Test: `backend/internal/proxy/proxy_helpers_test.go:401-410` (`TestAbortErrorContent` 수정)
- Test: `backend/internal/proxy/interruption_test.go:160-189` (`TestNonStreamChatEmptyMessageStill200` 수정)

**Interfaces:**
- Consumes: Task 1의 `isUpstreamErrorContent` (브라켓 매치 포함)
- Produces: `abortErrorContent(c, errMsg)`가 "Empty message"/"[Empty message]"도 `apiresponse.AbortServiceUnavailable` (503)로 응답

- [ ] **Step 1: `abortErrorContent` 수정**

현재:

```go
func abortErrorContent(c *gin.Context, errMsg string) {
	errFmt := apiresponse.FormatFromContext(c)
	if isInterruptionText(errMsg) {
		apiresponse.AbortServiceUnavailable(c, errFmt, errMsg)
		return
	}
	apiresponse.Abort(c, http.StatusOK, errFmt, "api_error", errMsg, "", "")
}
```

다음으로 교체 (레거시 200 분기 제거):

```go
func abortErrorContent(c *gin.Context, errMsg string) {
	errFmt := apiresponse.FormatFromContext(c)
	if isInterruptionText(errMsg) || isUpstreamErrorContent(errMsg) {
		apiresponse.AbortServiceUnavailable(c, errFmt, errMsg)
		return
	}
	apiresponse.Abort(c, http.StatusOK, errFmt, "api_error", errMsg, "", "")
}
```

- [ ] **Step 2: `TestAbortErrorContent`의 레거시 검증부 수정** (`proxy_helpers_test.go:401-410`)

현재 `w2` 블록 (200/`api_error` 기대)을 다음으로 교체:

```go
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	abortErrorContent(c2, "[Empty message]")
	if w2.Code != http.StatusServiceUnavailable {
		t.Errorf("empty-message status = %d, want 503", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), `"type":"overloaded_error"`) {
		t.Errorf("empty-message body = %s", w2.Body.String())
	}
```

- [ ] **Step 3: `TestNonStreamChatEmptyMessageStill200` → `TestNonStreamChatEmptyMessage503`** (`interruption_test.go`)

기대값 변경 (`/v1/chat/completions` → OpenAI 형식):

```go
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"message":"Empty message"`) {
		t.Errorf("body = %s", w.Body.String())
	}
```

- [ ] **Step 4: 테스트 실행**

Run: `go test ./internal/proxy/ -run 'AbortErrorContent|EmptyMessage503|EmptyMessageStill200' -v` (backend/ 에서)
Expected: 모두 PASS (개명한 테스트는 새 이름으로 통과)

- [ ] **Step 5: 커밋**

```bash
git add backend/internal/proxy/proxy_helpers.go backend/internal/proxy/proxy_helpers_test.go backend/internal/proxy/interruption_test.go
git commit -m "feat: convert non-stream Empty message to 503"
```

---

### Task 3: Responses-API 형식 스트림에서 "Empty message" 감지 (버퍼링 + 누적)

**Files:**
- Modify: `backend/internal/proxy/stream.go:114-255` (`handleStreamResponse`), `:257-304` (`handleRawStreamResponse`)
- Create: `backend/internal/proxy/responses_empty_test.go` (신규 테스트 4개)

**Interfaces:**
- Consumes: Task 1의 `isUpstreamErrorContent`, `writeStreamUpstreamError`; 기존 `randomID` 없음(테스트는 직접 SSE 구성)
- Produces: `extractResponsesOutputText(chunk string, deltaAccum *strings.Builder, finalText *string)` — Responses SSE 이벤트에서 출력 텍스트 누적. 두 핸들러 모두 응답 후반에 이 헬퍼와 `responsesMode`/`respBuf`/`respDeltaAccum`/`respFinalText` 상태를 사용

- [ ] **Step 1: 헬퍼 추가** (`stream.go` 끝, `extractDeltaContent` 아래)

```go
// extractResponsesOutputText accumulates output text from Responses-API SSE
// events. deltaAccum grows with output_text.delta deltas; finalText holds the
// authoritative full text once output_text.done or response.completed arrives
// (deltas alone are not enough — some upstreams skip the delta events).
func extractResponsesOutputText(chunk string, deltaAccum *strings.Builder, finalText *string) {
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev["type"] {
		case "response.output_text.delta":
			if d, ok := ev["delta"].(string); ok {
				deltaAccum.WriteString(d)
			}
		case "response.output_text.done":
			if t, ok := ev["text"].(string); ok {
				*finalText = t
			}
		case "response.completed":
			if resp, ok := ev["response"].(map[string]interface{}); ok {
				if t, ok := resp["output_text"].(string); ok {
					*finalText = t
				}
			}
		}
	}
}
```

- [ ] **Step 2: `handleStreamResponse`에 Responses 형식 버퍼링 추가**

읽기 루프 앞(기존 `var head bytes.Buffer` 선언 근처, `stream.go:146-149`)에 상태 추가:

```go
	// Responses-API format streams (e.g. conduit relay) carry the "Empty
	// message" failure text in output_text.delta events, split across chunks.
	// Buffer the whole stream and decide at the end so the failure can still
	// become a clean 503.
	responsesMode := false
	var respBuf bytes.Buffer
	var respDeltaAccum strings.Builder
	respFinalText := ""
```

읽기 루프 내부, `state["upstream_error"]` 체크 (`stream.go:178-180`) 직후에 분기 추가:

```go
			if !responsesMode && strings.Contains(chunkStr, `"type":"response.`) {
				responsesMode = true
			}
			if responsesMode {
				respBuf.Write(chunk)
				extractResponsesOutputText(chunkStr, &respDeltaAccum, &respFinalText)
				continue
			}
```

루프 종료 후, 기존 `state["upstream_error"]` 블록 (`stream.go:211-215`) 직후에 판정 블록 추가:

```go
	if responsesMode {
		text := respFinalText
		if text == "" {
			text = respDeltaAccum.String()
		}
		if isUpstreamErrorContent(text) {
			e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, text, latencyMs)
			writeStreamUpstreamError(c, text)
			return
		}
	}

	flushHead()

	if responsesMode {
		sw.Write(respBuf.Bytes())
	}
```

기존 `flushHead()` 호출(기존 216행)은 위 구조로 대체 — responsesMode가 아닐 때는 기존처럼 `flushHead()`만 실행된다. `sentDone`/사용량 로깅 후반부는 변경 없음.

- [ ] **Step 3: `handleRawStreamResponse`에도 동일 버퍼링 추가**

기존 루프(`stream.go:287-295`) 교체:

```go
	responsesMode := false
	var respBuf bytes.Buffer
	var respDeltaAccum strings.Builder
	respFinalText := ""

	for {
		if n > 0 {
			chunk := buf[:n]
			chunkStr := string(chunk)
			if !responsesMode && strings.Contains(chunkStr, `"type":"response.`) {
				responsesMode = true
			}
			if responsesMode {
				respBuf.Write(chunk)
				extractResponsesOutputText(chunkStr, &respDeltaAccum, &respFinalText)
				continue
			}
			sw.Write(chunk)
		}
		if err != nil {
			break
		}
		n, err = resp.Body.Read(buf)
	}

	if responsesMode {
		text := respFinalText
		if text == "" {
			text = respDeltaAccum.String()
		}
		if isUpstreamErrorContent(text) {
			e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, text, time.Since(start).Milliseconds())
			writeStreamUpstreamError(c, text)
			return
		}
		sw.Write(respBuf.Bytes())
	}
```

이후 사용량 로깅 후반부는 변경 없음.

- [ ] **Step 4: 신규 테스트 파일 작성** (`responses_empty_test.go`)

기존 `interruption_test.go`의 세팅 패턴(임시 DB + `NewEngine(nil, nil, usageSvc, nil, nil)` + `gin.CreateTestContext` + 수동 `http.Response`)을 그대로 사용한다.

```go
package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

func newResponsesEmptyEngine(t *testing.T) (*Engine, *service.UsageService) {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	return NewEngine(nil, nil, usageSvc, nil, nil), usageSvc
}

func responsesEmptySSE(parts string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(parts)),
	}
}

func TestHandleStreamResponseResponsesEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"[Empty\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" message]\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"[Empty message]\",\"annotations\":[]}]}],\"output_text\":\"[Empty message]\"}}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, responsesEmptySSE(sse), &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "data: ") {
		t.Errorf("stream data leaked into error body: %s", w.Body.String())
	}
}

func TestHandleStreamResponseResponsesDoneOnlyEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Some upstreams skip delta events and only send the final text.
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, responsesEmptySSE(sse), &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHandleStreamResponseResponsesNormalStream200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hi there\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"hi there\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hi there\",\"annotations\":[]}]}],\"output_text\":\"hi there\"}}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleStreamResponse(c, responsesEmptySSE(sse), &OpenAIAdapter{}, 1, 1, "conduit/gpt-5.6-sol", nil, 1, "openai", 5)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"delta":"hi there"`) {
		t.Errorf("buffered stream not replayed: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Errorf("missing [DONE]: %s", w.Body.String())
	}
}

func TestHandleRawStreamResponseResponsesEmptyMessage503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"gpt-5.6-sol\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"[Empty\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\" message]\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"text\":\"[Empty message]\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[],\"output_text\":\"[Empty message]\"}}\n\n" +
		"data: [DONE]\n\n"

	engine, _ := newResponsesEmptyEngine(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/conduit/v1/responses", nil)
	c.Set("request_id", "req-1")
	engine.handleRawStreamResponse(c, responsesEmptySSE(sse), 1, 1, "conduit/gpt-5.6-sol", time.Now(), 1)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}
```

- [ ] **Step 5: 테스트 실행**

Run: `go test ./internal/proxy/ -run 'ResponsesEmptyMessage|ResponsesDoneOnly|ResponsesNormal|RawStreamResponseResponses' -v` (backend/ 에서)
Expected: 4개 모두 PASS

- [ ] **Step 6: 전체 테스트 + vet**

Run: `go test ./... && go vet ./...` (backend/ 에서)
Expected: 전체 PASS, vet 경고 없음

- [ ] **Step 7: 커밋**

```bash
git add backend/internal/proxy/stream.go backend/internal/proxy/responses_empty_test.go
git commit -m "feat: detect Empty message in Responses-format streams"
```

---

### Task 4: 비스트리밍 Responses 형식 응답 감지 (`extractErrorContent` 확장)

**Files:**
- Modify: `backend/internal/proxy/proxy_helpers.go:58-89` (`extractErrorContent`)
- Test: `backend/internal/proxy/proxy_helpers_test.go` (신규 테스트 추가)

**Interfaces:**
- Consumes: Task 1의 `isErrorContent`/`isUpstreamErrorContent`, Task 2의 `abortErrorContent`
- Produces: `extractErrorContent(response map[string]interface{}) string`가 Responses 형식 `output[].content[].text`도 검사. `handlePathRoutedProxy`의 비스트리밍 경로(`proxy.go:497`)가 자동으로 503 처리

- [ ] **Step 1: `extractErrorContent`에 Responses 형식 분기 추가**

현재 함수 끝(choices 분기 뒤, `return ""` 앞)에 추가:

```go
	// Responses API format: output[].content[].text
	if output, ok := response["output"].([]interface{}); ok {
		for _, rawItem := range output {
			item, _ := rawItem.(map[string]interface{})
			if item["type"] != "message" {
				continue
			}
			content, _ := item["content"].([]interface{})
			for _, rawPart := range content {
				part, _ := rawPart.(map[string]interface{})
				if text, ok := part["text"].(string); ok && isErrorContent(text) {
					return text
				}
			}
		}
	}
```

- [ ] **Step 2: 단위 테스트 추가** (`proxy_helpers_test.go`)

```go
func TestExtractErrorContentResponsesFormat(t *testing.T) {
	resp := map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{
				"type": "message",
				"content": []interface{}{
					map[string]interface{}{"type": "output_text", "text": "[Empty message]", "annotations": []interface{}{}},
				},
			},
		},
	}
	if got := extractErrorContent(resp); got != "[Empty message]" {
		t.Errorf("responses format: extractErrorContent = %q, want %q", got, "[Empty message]")
	}

	normal := map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{
				"type": "message",
				"content": []interface{}{
					map[string]interface{}{"type": "output_text", "text": "normal reply", "annotations": []interface{}{}},
				},
			},
		},
	}
	if got := extractErrorContent(normal); got != "" {
		t.Errorf("normal responses: extractErrorContent = %q, want empty", got)
	}
}
```

- [ ] **Step 3: 테스트 실행**

Run: `go test ./internal/proxy/ -run 'ExtractErrorContent' -v` (backend/ 에서)
Expected: PASS

- [ ] **Step 4: 전체 테스트 + vet**

Run: `go test ./... && go vet ./...` (backend/ 에서)
Expected: 전체 PASS, vet 경고 없음

- [ ] **Step 5: 커밋**

```bash
git add backend/internal/proxy/proxy_helpers.go backend/internal/proxy/proxy_helpers_test.go
git commit -m "feat: detect Empty message in non-stream Responses responses"
```

---

## 셀프 리뷰 결과

- **스펙 커버리지**: 스트림 에러 503 병합(Task 1), 비스트리밍 일관화(Task 2), Responses 형식 스트림 버퍼링 감지(Task 3), 비스트리밍 Responses 감지(Task 4) — 스펙의 변경 사항 표 4행 모두 대응
- **플레이스홀더 없음**: 모든 스텝에 실제 코드 포함
- **타입 일관성**: `extractResponsesOutputText(chunk, *strings.Builder, *string)`, `isUpstreamErrorContent`, `writeStreamUpstreamError` 시그니처가 Task 간 동일
