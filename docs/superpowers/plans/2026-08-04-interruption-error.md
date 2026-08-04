# Anthropic interruption 에러 → 표준 503 변환 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Anthropic의 interruption 에러("Temporary service interruption. Retry the last turn; your conversation and tool state are preserved.")가 어떤 형태(비스트리밍 content, 스트리밍 text 델타, SSE `type: error` 이벤트)로든 도착하면 표준 503 retryable 에러(Anthropic `overloaded_error` / OpenAI `server_error`)로 변환해 에이전트 도구가 재시도할 수 있게 한다.

**Architecture:** 공유 감지 헬퍼(`isInterruptionText` 접두사 매치) + `apiresponse.AbortServiceUnavailable`(503)를 추가하고, 기존 "Request failed." 감지 메커니즘(`state["upstream_error"]` → 스트림 종료 후 표준 에러 응답)을 재사용한다. OpenAI-형식 스트림(`handleStreamResponse`)과 Responses 스트림(`handleResponsesStream`)은 첫 콘텐츠 전까지 출력을 버퍼링해 503을 되돌릴 수 있게 한다. 기존 "Request failed."(502)와 "Empty message"(200) 동작은 유지한다.

**Tech Stack:** Go 1.25, Gin, 기존 패키지(`internal/proxy`, `internal/apiresponse`) — 새 의존성 없음.

## Global Constraints

- 스펙: `docs/superpowers/specs/2026-08-04-interruption-error-design.md`
- `go test ./...`와 `go vet ./...`는 backend/에서 실행
- 커밋 메시지는 Conventional Commits (`feat:`, `fix:`, `test:` 등)
- 기존 "Request failed."(502 `api_error`)와 "Empty message"(200 `api_error`) 동작 변경 금지
- 새 함수명: `isInterruptionText`, `isErrorContent`, `abortErrorContent`, `writeStreamUpstreamError`, `AbortServiceUnavailable` — 태스크 간 시그니처 일치 필수
- 감지 문구는 접두사 매치: `strings.HasPrefix(strings.TrimSpace(text), "Temporary service interruption")`

---

### Task 1: `apiresponse.AbortServiceUnavailable` 헬퍼

**Files:**
- Modify: `backend/internal/apiresponse/errors.go:132` (AbortBadGateway 뒤)
- Create: `backend/internal/apiresponse/errors_test.go`

**Interfaces:**
- Produces: `func AbortServiceUnavailable(c *gin.Context, format Format, message string)` — Anthropic 형식: 503 + `"overloaded_error"`, 그 외(OpenAI): 503 + `"server_error"` + code `"upstream_error"`

- [ ] **Step 1: Write the failing test**

`backend/internal/apiresponse/errors_test.go` (신규):

```go
package apiresponse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAbortServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name   string
		format Format
		path   string
		want   string
	}{
		{"anthropic", FormatAnthropic, "/v1/messages", `"type":"overloaded_error"`},
		{"openai", FormatOpenAI, "/v1/chat/completions", `"type":"server_error"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			c.Set("request_id", "req-1")
			AbortServiceUnavailable(c, tc.format, "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved.")
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("body missing %s: %s", tc.want, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "Temporary service interruption") {
				t.Errorf("body missing message: %s", w.Body.String())
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/apiresponse/ -run TestAbortServiceUnavailable -v`
Expected: FAIL with "undefined: AbortServiceUnavailable"

- [ ] **Step 3: Write minimal implementation**

`backend/internal/apiresponse/errors.go` — `AbortBadGateway` 함수(132행) 바로 뒤에 추가:

```go
// AbortServiceUnavailable writes retryable temporary-outage errors in the
// appropriate format. Used for upstream service interruptions.
func AbortServiceUnavailable(c *gin.Context, format Format, message string) {
	switch format {
	case FormatAnthropic:
		Abort(c, http.StatusServiceUnavailable, format, "overloaded_error", message, "", "")
	default:
		Abort(c, http.StatusServiceUnavailable, format, "server_error", message, "upstream_error", "")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/apiresponse/ -run TestAbortServiceUnavailable -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/apiresponse/errors.go backend/internal/apiresponse/errors_test.go
git commit -m "feat: add AbortServiceUnavailable for retryable 503 errors"
```

---

### Task 2: 감지 헬퍼 (`isInterruptionText`, `isErrorContent`, `abortErrorContent`) + `extractErrorContent` 확장

**Files:**
- Modify: `backend/internal/proxy/proxy_helpers.go:23-60`
- Test: `backend/internal/proxy/proxy_helpers_test.go`

**Interfaces:**
- Consumes: `apiresponse.AbortServiceUnavailable` (Task 1)
- Produces:
  - `func isInterruptionText(text string) bool` — 접두사 매치 `"Temporary service interruption"`
  - `func isErrorContent(text string) bool` — `isUpstreamErrorContent(text) || isInterruptionText(text)`
  - `func abortErrorContent(c *gin.Context, errMsg string)` — interruption이면 503, 아니면 기존 200 `api_error` 동작 유지
  - `extractErrorContent`가 interruption 텍스트도 반환하도록 확장 (기존 시그니처 불변: `func extractErrorContent(response map[string]interface{}) string`)

- [ ] **Step 1: Write the failing test**

`backend/internal/proxy/proxy_helpers_test.go`에 추가 (파일 끝에):

```go
func TestIsInterruptionText(t *testing.T) {
	msg := "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."
	if !isInterruptionText(msg) {
		t.Error("exact message should match")
	}
	if !isInterruptionText("  " + msg + "  ") {
		t.Error("should be whitespace-tolerant")
	}
	if !isInterruptionText("Temporary service interruption.") {
		t.Error("prefix should match")
	}
	if isInterruptionText("Request failed.") {
		t.Error("other text must not match")
	}
	if isInterruptionText("") {
		t.Error("empty must not match")
	}
}

func TestExtractErrorContentInterruption(t *testing.T) {
	msg := "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."
	anthropic := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": msg},
		},
	}
	if got := extractErrorContent(anthropic); !isInterruptionText(got) {
		t.Errorf("anthropic content block: extractErrorContent = %q", got)
	}
	openai := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{"message": map[string]interface{}{"content": msg}},
		},
	}
	if got := extractErrorContent(openai); !isInterruptionText(got) {
		t.Errorf("choices message: extractErrorContent = %q", got)
	}
	if got := extractErrorContent(map[string]interface{}{
		"content": []interface{}{map[string]interface{}{"type": "text", "text": "normal reply"}},
	}); got != "" {
		t.Errorf("normal text: extractErrorContent = %q, want empty", got)
	}
}

func TestAbortErrorContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	msg := "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("request_id", "req-1")
	abortErrorContent(c, msg)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("interruption status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("interruption body = %s", w.Body.String())
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	abortErrorContent(c2, "Empty message")
	if w2.Code != http.StatusOK {
		t.Errorf("legacy status = %d, want 200", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), `"type":"api_error"`) {
		t.Errorf("legacy body = %s", w2.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run 'TestIsInterruptionText|TestExtractErrorContentInterruption|TestAbortErrorContent' -v`
Expected: FAIL with "undefined: isInterruptionText"

- [ ] **Step 3: Write minimal implementation**

`backend/internal/proxy/proxy_helpers.go` — `isUpstreamErrorContent`(23-27행) 뒤에 추가:

```go
// interruptionMarker is the prefix Anthropic uses for temporary service
// interruptions (error type "interruption").
const interruptionMarker = "Temporary service interruption"

// isInterruptionText reports whether text is Anthropic's temporary service
// interruption message. Prefix match so variants stay detected.
func isInterruptionText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), interruptionMarker)
}

// isErrorContent reports whether a response text is a known non-standard
// upstream error embedded in an otherwise successful response.
func isErrorContent(text string) bool {
	return isUpstreamErrorContent(text) || isInterruptionText(text)
}

// abortErrorContent writes a standard error for upstream error text embedded
// in a successful response. Interruptions are temporary → 503 retryable;
// other cases keep the legacy 200 api_error behavior.
func abortErrorContent(c *gin.Context, errMsg string) {
	errFmt := apiresponse.FormatFromContext(c)
	if isInterruptionText(errMsg) {
		apiresponse.AbortServiceUnavailable(c, errFmt, errMsg)
		return
	}
	apiresponse.Abort(c, http.StatusOK, errFmt, "api_error", errMsg, "", "")
}
```

`extractErrorContent`(31-60행) 내부의 `isUpstreamErrorContent(text)` 호출 3곳을 모두 `isErrorContent(text)`로 교체 (기존 시그니처와 구조 유지).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/proxy/ -run 'TestIsInterruptionText|TestExtractErrorContentInterruption|TestAbortErrorContent' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/proxy_helpers.go backend/internal/proxy/proxy_helpers_test.go
git commit -m "feat: detect Anthropic interruption text in proxy responses"
```

---

### Task 3: 비스트리밍 호출부 3곳 — interruption → 503

**Files:**
- Modify: `backend/internal/proxy/proxy.go:335-340` (executeMessages), `backend/internal/proxy/proxy.go:491-495` (handlePathRoutedProxy), `backend/internal/proxy/upstream.go:134-145` (parseNonStreamChatResponse)
- Create: `backend/internal/proxy/interruption_test.go` (라우트 레벨 테스트)

**Interfaces:**
- Consumes: `extractErrorContent`, `abortErrorContent` (Task 2)
- Produces: 세 경로 모두 interruption 콘텐츠 응답 시 503 + 형식별 에러

- [ ] **Step 1: Write the failing tests**

`backend/internal/proxy/interruption_test.go` (신규, `newResponsesTestRouter`(responses_test.go:284)와 `usage_logging_test.go`의 DB 시딩 패턴 재사용):

```go
package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

const interruptionMsg = "Temporary service interruption. Retry the last turn; your conversation and tool state are preserved."

func newInterruptionTestRouter(t *testing.T, upstream *httptest.Server, providerType string) *gin.Engine {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'p', 'P', ?, ?, ?, 1)`,
		upstream.URL, encryptTestAPIKey(t, "sk-test"), providerType,
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'm', 'p', 1, 1)`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, &config.Config{EncryptKey: testEncryptKey})
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleChatCompletions(c)
	})
	r.POST("/v1/messages", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleMessages(c)
	})
	r.Any("/:provider_key/v1/*endpoint", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandlePathRouted(c)
	})
	return r
}

func TestNonStreamChatInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1","object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"`+interruptionMsg+`"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "openai")

	body := `{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestNonStreamMessagesInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant",
			"content":[{"type":"text","text":"`+interruptionMsg+`"}],
			"model":"claude-opus-4-8","stop_reason":"end_turn",
			"usage":{"input_tokens":5,"output_tokens":2}
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "anthropic")

	body := `{"model":"p/m","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"overloaded_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestPathRoutedInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1","object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"`+interruptionMsg+`"},"finish_reason":"stop"}]
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "openai")

	body := `{"model":"p/unknown-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/p/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestNonStreamChatEmptyMessageStill200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1","object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"Empty message"},"finish_reason":"stop"}]
		}`)
	}))
	t.Cleanup(upstream.Close)

	r := newInterruptionTestRouter(t, upstream, "openai")

	body := `{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("legacy status = %d, body = %s", w.Code, w.Body.String())
	}
	var respObj map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respObj); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if errObj, ok := respObj["error"].(map[string]interface{}); !ok || errObj["message"] != "Empty message" {
		t.Errorf("expected legacy error body, got %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy/ -run 'TestNonStreamChatInterruption503|TestNonStreamMessagesInterruption503|TestPathRoutedInterruption503|TestNonStreamChatEmptyMessageStill200' -v`
Expected: `TestNonStreamChatInterruption503` FAIL (200 응답), `TestNonStreamMessagesInterruption503` FAIL (200 응답), `TestPathRoutedInterruption503` FAIL (200 응답), `TestNonStreamChatEmptyMessageStill200` FAIL (503 또는 200 정상 completion)

- [ ] **Step 3: Implement the three call-site changes**

**3a. `backend/internal/proxy/proxy.go` — executeMessages (335-340행)의 에러 블록:**

기존:
```go
	if errMsg := extractErrorContent(finalResponse); errMsg != "" {
		e.logUpstreamError(u, errMsg, latencyMs)
		errFmt := apiresponse.FormatFromContext(c)
		apiresponse.Abort(c, resp.StatusCode, errFmt, "api_error", errMsg, "", "")
		return
	}
```
변경:
```go
	if errMsg := extractErrorContent(finalResponse); errMsg != "" {
		e.logUpstreamError(u, errMsg, latencyMs)
		abortErrorContent(c, errMsg)
		return
	}
```

**3b. `backend/internal/proxy/proxy.go` — handlePathRoutedProxy (491-495행)의 에러 블록:**

기존:
```go
		if errMsg := extractErrorContent(respJSON); errMsg != "" {
			e.logUpstreamError(u, errMsg, latencyMs)
			apiresponse.Abort(c, resp.StatusCode, errFmt, "api_error", errMsg, "", "")
			return
		}
```
변경:
```go
		if errMsg := extractErrorContent(respJSON); errMsg != "" {
			e.logUpstreamError(u, errMsg, latencyMs)
			abortErrorContent(c, errMsg)
			return
		}
```

**3c. `backend/internal/proxy/upstream.go` — parseNonStreamChatResponse (134-145행):**

기존:
```go
	finalResponse, err := adapter.ParseChatResponse(modelResponse)
	if err != nil {
		c.Data(http.StatusOK, contentTypeOrDefault(respHeader), respBody)
		return nil, true
	}

	finalResponse["model"] = fullModelID

	// Count output tokens locally from the response content
	localOutput := countOutputTokens(finalResponse, providerType, fullModelID)
	latencyMs := time.Since(startTime).Milliseconds()
	completedAt := time.Now()
```
변경:
```go
	finalResponse, err := adapter.ParseChatResponse(modelResponse)
	if err != nil {
		c.Data(http.StatusOK, contentTypeOrDefault(respHeader), respBody)
		return nil, true
	}

	latencyMs := time.Since(startTime).Milliseconds()

	// Upstream errors embedded in a successful response (e.g. Anthropic
	// interruptions) become standard errors.
	if errMsg := extractErrorContent(finalResponse); errMsg != "" {
		usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &providerID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: errMsg,
			LatencyMs:    latencyMs,
			UserID:       &userID,
		})
		abortErrorContent(c, errMsg)
		return nil, true
	}

	finalResponse["model"] = fullModelID

	// Count output tokens locally from the response content
	localOutput := countOutputTokens(finalResponse, providerType, fullModelID)
	completedAt := time.Now()
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/proxy/ -run 'TestNonStreamChatInterruption503|TestNonStreamMessagesInterruption503|TestPathRoutedInterruption503|TestNonStreamChatEmptyMessageStill200' -v`
Expected: 모두 PASS

- [ ] **Step 5: Run full suite**

Run: `go test ./... && go vet ./...`
Expected: 전부 통과

- [ ] **Step 6: Commit**

```bash
git add backend/internal/proxy/proxy.go backend/internal/proxy/upstream.go backend/internal/proxy/interruption_test.go
git commit -m "feat: convert embedded interruption content to 503 in non-streaming paths"
```

---

### Task 4: `ParseMessagesStreamChunk` 감지 (Anthropic `/v1/messages` 스트림)

**Files:**
- Modify: `backend/internal/proxy/anthropic_adapter.go:361-369` (ParseMessagesStreamChunk)
- Test: `backend/internal/proxy/anthropic_adapter_test.go`

**Interfaces:**
- Consumes: `isInterruptionText` (Task 2)
- Produces: interruption 감지 시 `state["upstream_error"] = <메시지>` 설정 + `nil` 반환 (청크 드롭). "Request failed." 기존 동작 유지. SSE `type: error` 이벤트의 `error.type == "interruption"` 또는 메시지 접두사 매치 처리

- [ ] **Step 1: Write the failing tests**

`backend/internal/proxy/anthropic_adapter_test.go`에 추가:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy/ -run 'TestParseMessagesStreamChunkInterruption|TestParseMessagesStreamChunkOtherErrorEvent' -v`
Expected: FAIL (upstream_error 미설정, out이 nil 아님)

- [ ] **Step 3: Write minimal implementation**

`backend/internal/proxy/anthropic_adapter.go` — ParseMessagesStreamChunk (361-369행)의 switch:

기존:
```go
		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
					if text, _ := delta["text"].(string); text == "Request failed." {
						state["upstream_error"] = text
						return nil, inputTokens, outputTokens, nil
					}
				}
			}
```
변경:
```go
		case "content_block_delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
					if text, _ := delta["text"].(string); text == "Request failed." || isInterruptionText(text) {
						state["upstream_error"] = text
						return nil, inputTokens, outputTokens, nil
					}
				}
			}
		case "error":
			if errObj, ok := event["error"].(map[string]interface{}); ok {
				if msg := vstr(errObj, "message"); isInterruptionText(msg) {
					state["upstream_error"] = msg
					return nil, inputTokens, outputTokens, nil
				}
			}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/proxy/ -run 'TestParseMessagesStreamChunk|TestParseStreamChunk' -v`
Expected: 모두 PASS (기존 테스트 포함)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/anthropic_adapter.go backend/internal/proxy/anthropic_adapter_test.go
git commit -m "feat: detect Anthropic interruption in messages stream adapter"
```

---

### Task 5: `ParseStreamChunk` 감지 (Anthropic → OpenAI 형식 스트림)

**Files:**
- Modify: `backend/internal/proxy/anthropic_adapter.go:248-257` (ParseStreamChunk)
- Test: `backend/internal/proxy/anthropic_adapter_test.go`

**Interfaces:**
- Consumes: `isInterruptionText` (Task 2)
- Produces: interruption 감지 시 `state["upstream_error"] = <메시지>` + `nil` 반환 (OpenAI chunk 변환 전 드롭). 정상 text 델타는 기존 변환 유지

- [ ] **Step 1: Write the failing tests**

`backend/internal/proxy/anthropic_adapter_test.go`에 추가:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy/ -run 'TestParseStreamChunkInterruption|TestParseStreamChunkNormalDelta' -v`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

`backend/internal/proxy/anthropic_adapter.go` — ParseStreamChunk (248-257행)의 switch:

기존:
```go
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]interface{})
			if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
				textDelta, _ := delta["text"].(string)
				events = append(events, map[string]interface{}{
					"choices": []map[string]interface{}{
						{"index": 0, "delta": map[string]interface{}{"content": textDelta}},
					},
				})
			}
```
변경:
```go
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]interface{})
			if deltaType, _ := delta["type"].(string); deltaType == "text_delta" {
				textDelta, _ := delta["text"].(string)
				if isInterruptionText(textDelta) {
					state["upstream_error"] = textDelta
					return nil, inputTokens, outputTokens, nil
				}
				events = append(events, map[string]interface{}{
					"choices": []map[string]interface{}{
						{"index": 0, "delta": map[string]interface{}{"content": textDelta}},
					},
				})
			}
		case "error":
			if errObj, ok := event["error"].(map[string]interface{}); ok {
				if msg := vstr(errObj, "message"); isInterruptionText(msg) {
					state["upstream_error"] = msg
					return nil, inputTokens, outputTokens, nil
				}
			}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/proxy/ -run 'TestParseMessagesStreamChunk|TestParseStreamChunk' -v`
Expected: 모두 PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/anthropic_adapter.go backend/internal/proxy/anthropic_adapter_test.go
git commit -m "feat: detect Anthropic interruption in chat stream adapter"
```

---

### Task 6: 스트림 핸들러 — `writeStreamUpstreamError` + 헤드 버퍼링

**Files:**
- Modify: `backend/internal/proxy/stream.go:59-164` (handleStreamResponse), `backend/internal/proxy/stream.go:244-253` (handleMessagesStreamResponse)
- Test: `backend/internal/proxy/interruption_test.go` (Task 3에서 만든 파일에 추가)

**Interfaces:**
- Consumes: `isInterruptionText`, `reformatError`, `upstreamError` (기존), Task 4/5의 `state["upstream_error"]`
- Produces:
  - `func writeStreamUpstreamError(c *gin.Context, errMsg string)` — interruption: 503 + `overloaded_error`(Anthropic 형식) / `server_error`(OpenAI 형식) + `code:"upstream_error"`; 그 외: 기존 502 + `api_error`
  - `handleStreamResponse`: `state["upstream_error"]` 확인 시 중단 + 첫 콘텐츠 전까지 헤드 버퍼링
  - `handleMessagesStreamResponse`: 기존 502 블록을 `writeStreamUpstreamError`로 대체

- [ ] **Step 1: Write the failing tests**

`backend/internal/proxy/interruption_test.go`에 추가:

```go
func TestHandleStreamResponseInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + interruptionMsg + "\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, 1, "anthropic", 5)

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

func TestHandleMessagesStreamResponseInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + interruptionMsg + "\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleMessagesStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, time.Now(), 1, "anthropic", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"overloaded_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHandleMessagesStreamResponseRequestFailedStill502(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Request failed.\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleMessagesStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, time.Now(), 1, "anthropic", 5)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"api_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestHandleStreamResponseNormalStreamStill200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi there\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleStreamResponse(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, 1, "anthropic", 5)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"content":"hi there"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Errorf("missing [DONE]: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy/ -run 'TestHandleStreamResponse|TestHandleMessagesStreamResponse' -v`
Expected: interruption 테스트 FAIL (200 + 스트림 그대로), `RequestFailedStill502`는 PASS (기존 동작), `NormalStreamStill200` FAIL (버퍼링으로 [DONE] 누락 또는 상태 이상)

- [ ] **Step 3: Add `writeStreamUpstreamError` helper**

`backend/internal/proxy/stream.go` — `handleStreamResponse` 함수(59행) 앞에 추가:

```go
// writeStreamUpstreamError converts an upstream error detected mid-stream
// into a standard error response. Interruptions are temporary → 503
// retryable (overloaded_error/server_error); other upstream errors keep the
// legacy 502 api_error shape.
func writeStreamUpstreamError(c *gin.Context, errMsg string) {
	errFmt := apiresponse.FormatFromContext(c)
	requestID := c.GetString("request_id")
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
	c.Status(http.StatusBadGateway)
	c.Header("Content-Type", "application/json")
	c.Writer.Write(reformatError(upstreamError{ErrType: "api_error", Message: errMsg}, errFmt, requestID))
}
```

- [ ] **Step 4: Rework `handleStreamResponse` with head buffering**

`backend/internal/proxy/stream.go:90-131` — 현재 루프 + 종료 처리:

기존 (90-131행):
```go
	var totalInputTokens, totalOutputTokens int64
	var outputTextAccum strings.Builder
	sentDone := false

	for {
		if n > 0 {
			chunk := buf[:n]
			chunkStr := string(chunk)
			if strings.Contains(chunkStr, "data: [DONE]") {
				sentDone = true
			}

			extractDeltaContent(chunkStr, providerType, &outputTextAccum)

			transformed, inTok, outTok, _ := adapter.ParseStreamChunk(chunk, state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}
			if transformed != nil {
				if strings.Contains(string(transformed), "data: [DONE]") {
					sentDone = true
				}
				sw.Write(transformed)
			} else {
				sw.Write(chunk)
			}
		}
		if err != nil {
			break
		}
		n, err = resp.Body.Read(buf)
	}

	if !sentDone {
		sw.Write([]byte("data: [DONE]\n\n"))
	}

	latencyMs := time.Since(start).Milliseconds()
	completedAt := time.Now()
```

변경:
```go
	var totalInputTokens, totalOutputTokens int64
	var outputTextAccum strings.Builder
	var head bytes.Buffer
	headOpen := true
	sentDone := false

	flushHead := func() {
		if headOpen {
			sw.Write(head.Bytes())
			head.Reset()
			headOpen = false
		}
	}

	for {
		if n > 0 {
			chunk := buf[:n]
			chunkStr := string(chunk)
			if strings.Contains(chunkStr, "data: [DONE]") {
				sentDone = true
			}

			extractDeltaContent(chunkStr, providerType, &outputTextAccum)

			transformed, inTok, outTok, _ := adapter.ParseStreamChunk(chunk, state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}
			// Upstream error (e.g. Anthropic interruption) detected mid-stream:
			// abort before anything content-bearing reaches the client.
			if _, errSet := state["upstream_error"]; errSet {
				break
			}

			var out []byte
			if transformed != nil {
				if strings.Contains(string(transformed), "data: [DONE]") {
					sentDone = true
				}
				out = transformed
			} else {
				out = chunk
			}
			// Hold the pre-content head (role/empty chunks) until the first
			// content-bearing chunk so a mid-stream upstream error can still
			// become a clean 503.
			if headOpen && (strings.Contains(string(out), "content") || strings.Contains(string(out), "finish_reason") || sentDone) {
				flushHead()
			}
			if headOpen {
				head.Write(out)
			} else {
				sw.Write(out)
			}
		}
		if err != nil {
			break
		}
		n, err = resp.Body.Read(buf)
	}

	latencyMs := time.Since(start).Milliseconds()

	if errMsg, ok := state["upstream_error"].(string); ok {
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, errMsg, latencyMs)
		writeStreamUpstreamError(c, errMsg)
		return
	}
	flushHead()

	if !sentDone {
		sw.Write([]byte("data: [DONE]\n\n"))
	}

	completedAt := time.Now()
```

- [ ] **Step 5: Replace `handleMessagesStreamResponse` error block**

`backend/internal/proxy/stream.go:244-253` — 기존:
```go
	if _, ok := state["upstream_error"]; ok {
		errMsg, _ := state["upstream_error"].(string)
		errFmt := apiresponse.FormatFromContext(c)
		requestID := c.GetString("request_id")
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, errMsg, time.Since(start).Milliseconds())
		c.Status(http.StatusBadGateway)
		c.Header("Content-Type", "application/json")
		c.Writer.Write(reformatError(upstreamError{ErrType: "api_error", Message: errMsg}, errFmt, requestID))
		return
	}
```
변경:
```go
	if errMsg, ok := state["upstream_error"].(string); ok {
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, errMsg, time.Since(start).Milliseconds())
		writeStreamUpstreamError(c, errMsg)
		return
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/proxy/ -run 'TestHandleStreamResponse|TestHandleMessagesStreamResponse' -v`
Expected: 모두 PASS

- [ ] **Step 7: Run full suite**

Run: `go test ./... && go vet ./...`
Expected: 전부 통과

- [ ] **Step 8: Commit**

```bash
git add backend/internal/proxy/stream.go backend/internal/proxy/interruption_test.go
git commit -m "feat: convert mid-stream Anthropic interruption to 503 in stream handlers"
```

---

### Task 7: `handleResponsesStream` — 이벤트 지연 emit + 503

**Files:**
- Modify: `backend/internal/proxy/responses_stream.go:50-63` (emit), `:131-140` (루프), `:194-206` (콘텐츠/finish 분기), `:216-262` (종료 처리)
- Test: `backend/internal/proxy/responses_test.go`

**Interfaces:**
- Consumes: `writeStreamUpstreamError` (Task 6), Task 5의 `state["upstream_error"]`
- Produces: interruption 스트림 → 503, 정상 스트림은 기존 이벤트 순서 유지

- [ ] **Step 1: Write the failing test**

`backend/internal/proxy/responses_test.go`에 추가:

```go
func TestHandleResponsesStreamInterruption503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sse := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + interruptionMsg + "\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("request_id", "req-1")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleResponsesStream(c, upstream, &AnthropicAdapter{}, 1, 1, "anthropic/claude-opus-4-8", nil, 1, "anthropic", 5)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"server_error"`) {
		t.Errorf("body = %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "response.created") {
		t.Errorf("response events leaked into error body: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/ -run TestHandleResponsesStreamInterruption503 -v`
Expected: FAIL (200 + response.created 이벤트가 나감)

- [ ] **Step 3: Implement emit deferral**

`backend/internal/proxy/responses_stream.go:50-53` — 기존:
```go
	emit := func(ev map[string]interface{}) {
		b, _ := json.Marshal(ev)
		sw.Write([]byte("data: " + string(b) + "\n\n"))
	}
```
변경:
```go
	// Pre-content events are held back until the first content-bearing chunk
	// so a mid-stream upstream error can still become a clean 503.
	var pendingEvents [][]byte
	contentStarted := false
	emit := func(ev map[string]interface{}) {
		b, _ := json.Marshal(ev)
		data := []byte("data: " + string(b) + "\n\n")
		if contentStarted {
			sw.Write(data)
			return
		}
		pendingEvents = append(pendingEvents, data)
	}
	flushPending := func() {
		if len(pendingEvents) > 0 {
			sw.Write(bytes.Join(pendingEvents, nil))
			pendingEvents = nil
		}
	}
```

- [ ] **Step 4: Add upstream_error break in the loop**

`backend/internal/proxy/responses_stream.go:134-140` — `ParseStreamChunk` 호출 직후에 추가:
```go
			transformed, inTok, outTok, _ := adapter.ParseStreamChunk(chunk, state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}
			// Upstream error (e.g. Anthropic interruption): abort before
			// emitting any response events.
			if _, errSet := state["upstream_error"]; errSet {
				break
			}
```

- [ ] **Step 5: Flush on first content / finish_reason**

`backend/internal/proxy/responses_stream.go:194-206` — 기존:
```go
					if content, ok := delta["content"].(string); ok && content != "" {
						if current == nil || current.kind != "message" {
							closeItem()
							openMessage()
						}
						current.text.WriteString(content)
						outputTextAccum.WriteString(content)
						emit(map[string]interface{}{"type": "response.output_text.delta", "item_id": current.id, "output_index": len(outputItems), "content_index": 0, "delta": content})
					}

					if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
						finishReason = fr
					}
```
변경:
```go
					if content, ok := delta["content"].(string); ok && content != "" {
						if !contentStarted {
							contentStarted = true
							flushPending()
						}
						if current == nil || current.kind != "message" {
							closeItem()
							openMessage()
						}
						current.text.WriteString(content)
						outputTextAccum.WriteString(content)
						emit(map[string]interface{}{"type": "response.output_text.delta", "item_id": current.id, "output_index": len(outputItems), "content_index": 0, "delta": content})
					}

					if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
						if !contentStarted {
							contentStarted = true
							flushPending()
						}
						finishReason = fr
					}
```

- [ ] **Step 6: Error check + flush before `closeItem()`**

`backend/internal/proxy/responses_stream.go:216` — 기존:
```go
	closeItem()
```
변경 (latencyMs 계산을 위로 이동):
```go
	latencyMs := time.Since(start).Milliseconds()

	if errMsg, ok := state["upstream_error"].(string); ok {
		e.logUpstreamError(usageContext{apiKeyID: apiKeyID, providerID: providerID, userID: userID, fullModelID: fullModelID}, errMsg, latencyMs)
		writeStreamUpstreamError(c, errMsg)
		return
	}
	if !contentStarted {
		contentStarted = true
		flushPending()
	}

	closeItem()
```

그리고 262행의 기존 `latencyMs := time.Since(start).Milliseconds()` 중복 선언을 제거 (completedAt만 남김):
```go
	completedAt := time.Now()
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/proxy/ -run 'TestHandleResponsesStream' -v`
Expected: 신규 테스트 + 기존 4개(TestHandleResponsesNonStreaming, TestHandleResponsesStreaming, TestHandleResponsesStreamAnthropic, TestHandleResponsesStreamToolCall) 모두 PASS

- [ ] **Step 8: Run full suite**

Run: `go test ./... && go vet ./...`
Expected: 전부 통과

- [ ] **Step 9: Commit**

```bash
git add backend/internal/proxy/responses_stream.go backend/internal/proxy/responses_test.go
git commit -m "feat: convert mid-stream Anthropic interruption to 503 in responses stream"
```

---

## Self-Review 메모

- **스펙 커버리지**: 감지(헬퍼) → Task 2, 에러 형태(503/overloaded/server_error) → Task 1·6, 비스트리밍 3경로 → Task 3, Anthropic 스트림 → Task 4·6, OpenAI 스트림 → Task 5·6, Responses 스트림 → Task 7, 사용량 로깅(기존 패턴 유지) → Task 3(UsageLog IsError)·6·7(logUpstreamError)
- **미포함 (스펙 비범위)**: "Request failed." 502 유지 → Task 6 회귀 테스트로 검증, "Empty message" 200 유지 → Task 3 회귀 테스트로 검증
- **시그니처 일관성**: `isInterruptionText(string) bool`, `abortErrorContent(*gin.Context, string)`, `writeStreamUpstreamError(*gin.Context, string)`, `AbortServiceUnavailable(*gin.Context, Format, string)` — 모든 태스크가 동일 시그니처 사용
- **주의**: Task 6의 헤드 버퍼링 조건(`strings.Contains(out, "content")`)은 이 코드베이스 자체가 생성하는 chunk 형식(`"delta":{"content":...}` / `"finish_reason":...`)에 의존 — Anthropic adapter의 fallback chunk(`"object":"chat.completion.chunk"`)와 role chunk(`"delta":{"role":"assistant"}`)는 "content"를 포함하지 않으므로 버퍼에 남음
