# API Error Response Improvement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Normalize all API error responses to the client's expected format (OpenAI or Anthropic), add `request_id` for traceability, fix JWT middleware format, and emit SSE error events on stream interruption.

**Architecture:** New `upstream_error.go` file provides `parseUpstreamError` (provider-specific JSON extraction) and `reformatError` (format-aware serialization). `writeUpstreamErrorBody` becomes format-aware. `ensureRequestID` helper stores a UUID in gin context. All `Abort*` functions read `request_id` from context. JWT middleware uses structured error shape. Stream handlers emit SSE error events on unexpected EOF.

**Tech Stack:** Go 1.25, Gin, `github.com/google/uuid` (already indirect dep)

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/internal/proxy/upstream_error.go` | Create | `parseUpstreamError`, `reformatError`, `ensureRequestID` |
| `backend/internal/proxy/upstream_error_test.go` | Create | Tests for parsing and reformatting |
| `backend/internal/proxy/proxy_helpers.go` | Modify | `writeUpstreamErrorBody` signature + format-aware logic |
| `backend/internal/proxy/proxy.go` | Modify | `ensureRequestID` calls, stream error events |
| `backend/internal/apiresponse/errors.go` | Modify | `request_id` in OpenAI/Anthropic abort functions |
| `backend/internal/apiresponse/errors_test.go` | Modify | Add `request_id` assertions |
| `backend/internal/middleware/jwt_auth.go` | Modify | Structured error shape |
| `backend/go.mod` | Modify | Move `uuid` from indirect to direct |

---

## Task 1: Create `upstream_error.go` — parser and reformatter

**Files:**
- Create: `backend/internal/proxy/upstream_error.go`
- Test: `backend/internal/proxy/upstream_error_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/proxy/upstream_error_test.go`:

```go
package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"omnirelay/internal/apiresponse"
)

func TestParseUpstreamError_OpenAI(t *testing.T) {
	body := []byte(`{"error":{"message":"You exceeded your current quota","type":"rate_limit_exceeded","param":null,"code":"rate_limit_exceeded"}}`)
	parsed, ok := parseUpstreamError("openai", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "You exceeded your current quota" {
		t.Errorf("message = %q", parsed.Message)
	}
	if parsed.Code != "rate_limit_exceeded" {
		t.Errorf("code = %q", parsed.Code)
	}
}

func TestParseUpstreamError_Anthropic(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
	parsed, ok := parseUpstreamError("anthropic", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "rate limited" {
		t.Errorf("message = %q", parsed.Message)
	}
	if parsed.ErrType != "rate_limit_error" {
		t.Errorf("errType = %q", parsed.ErrType)
	}
}

func TestParseUpstreamError_Gemini(t *testing.T) {
	body := []byte(`{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`)
	parsed, ok := parseUpstreamError("gemini", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "Quota exceeded" {
		t.Errorf("message = %q", parsed.Message)
	}
	if parsed.Code != "429" && parsed.Code != "RESOURCE_EXHAUSTED" {
		t.Errorf("code = %q", parsed.Code)
	}
}

func TestParseUpstreamError_Fallback(t *testing.T) {
	body := []byte(`{"message":"something broke"}`)
	parsed, ok := parseUpstreamError("unknown", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "something broke" {
		t.Errorf("message = %q", parsed.Message)
	}
}

func TestParseUpstreamError_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	_, ok := parseUpstreamError("openai", body)
	if ok {
		t.Error("expected parse to fail on invalid JSON")
	}
}

func TestReformatError_OpenAI(t *testing.T) {
	err := upstreamError{ErrType: "rate_limit_exceeded", Message: "quota exceeded", Code: "rate_limit_exceeded"}
	result := reformatError(err, apiresponse.FormatOpenAI, "test-req-123")
	var resp map[string]interface{}
	if e := json.Unmarshal(result, &resp); e != nil {
		t.Fatal(e)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", resp)
	}
	if errObj["request_id"] != "test-req-123" {
		t.Errorf("request_id = %v", errObj["request_id"])
	}
	if errObj["type"] != "rate_limit_exceeded" {
		t.Errorf("type = %v", errObj["type"])
	}
}

func TestReformatError_Anthropic(t *testing.T) {
	err := upstreamError{ErrType: "rate_limit_error", Message: "rate limited"}
	result := reformatError(err, apiresponse.FormatAnthropic, "test-req-456")
	var resp map[string]interface{}
	if e := json.Unmarshal(result, &resp); e != nil {
		t.Fatal(e)
	}
	if resp["type"] != "error" {
		t.Errorf("type = %v", resp["type"])
	}
	if resp["request_id"] != "test-req-456" {
		t.Errorf("request_id = %v", resp["request_id"])
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", resp)
	}
	if errObj["type"] != "rate_limit_error" {
		t.Errorf("error.type = %v", errObj["type"])
	}
}

func TestEnsureRequestID_GeneratesUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	id := ensureRequestID(c)
	if id == "" {
		t.Error("expected non-empty request_id")
	}
	if c.GetString("request_id") != id {
		t.Error("request_id not stored in context")
	}
}

func TestEnsureRequestID_RespectsHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Request-Id", "client-provided-123")

	id := ensureRequestID(c)
	if id != "client-provided-123" {
		t.Errorf("expected client-provided ID, got %q", id)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/proxy/ -run "TestParseUpstreamError|TestReformatError|TestEnsureRequestID" -v`
Expected: FAIL — `parseUpstreamError`, `reformatError`, `ensureRequestID` not defined

- [ ] **Step 3: Implement `upstream_error.go`**

Create `backend/internal/proxy/upstream_error.go`:

```go
package proxy

import (
	"encoding/json"
	"omnirelay/internal/apiresponse"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// upstreamError is a provider-agnostic representation of an upstream error.
type upstreamError struct {
	ErrType string
	Message string
	Code    string
	Param   string
}

// parseUpstreamError extracts a normalized error from a provider's raw error JSON body.
// Returns (upstreamError, true) on success, (_, false) if the body is not a recognized shape.
func parseUpstreamError(providerType string, body []byte) (upstreamError, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return upstreamError{}, false
	}

	switch providerType {
	case "anthropic":
		return parseAnthropicError(raw)
	case "gemini":
		return parseGeminiError(raw)
	case "openai", "lmstudio", "ollama":
		return parseOpenAIError(raw)
	default:
		return parseGenericError(raw)
	}
}

func parseOpenAIError(raw map[string]interface{}) (upstreamError, bool) {
	errObj, ok := raw["error"].(map[string]interface{})
	if !ok {
		return parseGenericError(raw)
	}
	e := upstreamError{
		ErrType: vstr(errObj, "type"),
		Message: vstr(errObj, "message"),
		Code:    vstr(errObj, "code"),
		Param:   vstr(errObj, "param"),
	}
	if e.Message == "" {
		return upstreamError{}, false
	}
	return e, true
}

func parseAnthropicError(raw map[string]interface{}) (upstreamError, bool) {
	errObj, ok := raw["error"].(map[string]interface{})
	if !ok {
		return parseGenericError(raw)
	}
	e := upstreamError{
		ErrType: vstr(errObj, "type"),
		Message: vstr(errObj, "message"),
	}
	if e.Message == "" {
		return upstreamError{}, false
	}
	return e, true
}

func parseGeminiError(raw map[string]interface{}) (upstreamError, bool) {
	errObj, ok := raw["error"].(map[string]interface{})
	if !ok {
		return parseGenericError(raw)
	}
	e := upstreamError{
		Message: vstr(errObj, "message"),
		Code:    vstr(errObj, "status"),
	}
	if e.Code == "" {
		i, ok := errObj["code"].(float64)
		if ok {
			e.Code = jsonFloatToCode(i)
		}
	}
	if e.Message == "" {
		return upstreamError{}, false
	}
	return e, true
}

func parseGenericError(raw map[string]interface{}) (upstreamError, bool) {
	// Try OpenAI-style nested error first
	if errObj, ok := raw["error"].(map[string]interface{}); ok {
		if msg := vstr(errObj, "message"); msg != "" {
			return upstreamError{
				ErrType: vstr(errObj, "type"),
				Message: msg,
				Code:    vstr(errObj, "code"),
				Param:   vstr(errObj, "param"),
			}, true
		}
	}
	// Fallback: top-level "message"
	if msg, ok := raw["message"].(string); ok && msg != "" {
		return upstreamError{Message: msg}, true
	}
	return upstreamError{}, false
}

// reformatError serializes a normalized error into the target format's wire shape,
// including the request_id. Returns JSON-encoded bytes.
func reformatError(err upstreamError, targetFormat apiresponse.Format, requestID string) []byte {
	switch targetFormat {
	case apiresponse.FormatAnthropic:
		obj := map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    err.ErrType,
				"message": err.Message,
			},
			"request_id": requestID,
		}
		b, _ := json.Marshal(obj)
		return b
	default:
		obj := map[string]interface{}{
			"error": map[string]interface{}{
				"message":    err.Message,
				"type":       err.ErrType,
				"param":      nil,
				"code":       nil,
				"request_id": requestID,
			},
		}
		if err.Param != "" {
			obj["error"].(map[string]interface{})["param"] = err.Param
		}
		if err.Code != "" {
			obj["error"].(map[string]interface{})["code"] = err.Code
		}
		b, _ := json.Marshal(obj)
		return b
	}
}

// ensureRequestID reads the X-Request-Id header or generates a new UUID,
// stores it in the gin context, and returns it.
func ensureRequestID(c *gin.Context) string {
	if id := c.GetHeader("X-Request-Id"); id != "" {
		c.Set("request_id", id)
		return id
	}
	id := uuid.New().String()
	c.Set("request_id", id)
	return id
}

// vstr safely extracts a string value from a map.
func vstr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// jsonFloatToCode converts a JSON number (always float64) to a string code.
func jsonFloatToCode(f float64) string {
	return strconv.FormatInt(int64(f), 10)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/proxy/ -run "TestParseUpstreamError|TestReformatError|TestEnsureRequestID" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/upstream_error.go backend/internal/proxy/upstream_error_test.go
git commit -m "feat(proxy): add upstream error parser and reformatter with request_id support"
```

---

## Task 2: Update `writeUpstreamErrorBody` to be format-aware

**Files:**
- Modify: `backend/internal/proxy/proxy_helpers.go:60-63`
- Modify: `backend/internal/proxy/proxy_helpers.go:96`
- Modify: `backend/internal/proxy/proxy.go:647`

- [ ] **Step 1: Update `writeUpstreamErrorBody` signature and logic**

In `backend/internal/proxy/proxy_helpers.go`, replace the function at line 60:

```go
// readUpstreamError reads the body and writes an error response back to the client with the provider's status code.
// It normalizes the error to the client's expected format (OpenAI or Anthropic) and includes request_id.
func writeUpstreamErrorBody(c *gin.Context, resp *http.Response, providerType string) {
	respBody, _ := io.ReadAll(resp.Body)

	if parsed, ok := parseUpstreamError(providerType, respBody); ok {
		errFmt := apiresponse.FormatFromContext(c)
		requestID := c.GetString("request_id")
		normalized := reformatError(parsed, errFmt, requestID)
		c.Data(resp.StatusCode, "application/json", normalized)
		return
	}
	// Fallback: passthrough raw body when parsing fails
	c.Data(resp.StatusCode, contentTypeOrDefault(resp.Header), respBody)
}
```

- [ ] **Step 2: Update call site in `proxyJSONRequest` (line 96)**

In `backend/internal/proxy/proxy_helpers.go`, change line 96 from:
```go
		writeUpstreamErrorBody(c, resp)
```
to:
```go
		writeUpstreamErrorBody(c, resp, providerType)
```

- [ ] **Step 3: Update call site in `handlePathRoutedProxy` (line 647)**

In `backend/internal/proxy/proxy.go`, change line 647 from:
```go
		writeUpstreamErrorBody(c, resp)
```
to:
```go
		writeUpstreamErrorBody(c, resp, provider.ProviderType)
```

- [ ] **Step 4: Run tests to verify no regressions**

Run: `cd backend && go test ./internal/proxy/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/proxy_helpers.go backend/internal/proxy/proxy.go
git commit -m "feat(proxy): make writeUpstreamErrorBody format-aware with provider type"
```

---

## Task 3: Add `ensureRequestID` calls to proxy handlers

**Files:**
- Modify: `backend/internal/proxy/proxy.go:16,44,108`

- [ ] **Step 1: Add `ensureRequestID` to `HandleChatCompletions`**

In `backend/internal/proxy/proxy.go`, at the top of `HandleChatCompletions` (line 16), add after the opening brace:

```go
func (e *Engine) HandleChatCompletions(c *gin.Context) {
	ensureRequestID(c)
	apiKeyID := c.GetInt64("api_key_id")
```

- [ ] **Step 2: Add `ensureRequestID` to `HandleMessages`**

In `backend/internal/proxy/proxy.go`, at the top of `HandleMessages` (line 44), add after the opening brace:

```go
func (e *Engine) HandleMessages(c *gin.Context) {
	ensureRequestID(c)
	apiKeyID := c.GetInt64("api_key_id")
```

- [ ] **Step 3: Add `ensureRequestID` to `HandlePathRouted`**

In `backend/internal/proxy/proxy.go`, at the top of `HandlePathRouted` (line 108), add after the opening brace:

```go
func (e *Engine) HandlePathRouted(c *gin.Context) {
	ensureRequestID(c)
	providerKey := c.Param("provider_key")
```

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/proxy/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/proxy.go
git commit -m "feat(proxy): add ensureRequestID to all proxy handler entry points"
```

---

## Task 4: Add `request_id` to apiresponse error functions

**Files:**
- Modify: `backend/internal/apiresponse/errors.go:33-74,77-128`
- Modify: `backend/internal/apiresponse/errors_test.go`

- [ ] **Step 1: Update `abortOpenAI` to include `request_id`**

In `backend/internal/apiresponse/errors.go`, replace the `abortOpenAI` function (lines 43-60):

```go
func abortOpenAI(c *gin.Context, status int, errType, message, code, param string) {
	if errType == "" {
		errType = "invalid_request_error"
	}
	errObj := gin.H{
		"message": message,
		"type":    errType,
		"param":   nil,
		"code":    nil,
	}
	if param != "" {
		errObj["param"] = param
	}
	if code != "" {
		errObj["code"] = code
	}
	if requestID := c.GetString("request_id"); requestID != "" {
		errObj["request_id"] = requestID
	}
	c.JSON(status, gin.H{"error": errObj})
}
```

- [ ] **Step 2: Update `abortAnthropic` to populate `request_id`**

In `backend/internal/apiresponse/errors.go`, replace the `abortAnthropic` function (lines 62-74):

```go
func abortAnthropic(c *gin.Context, status int, errType, message string) {
	if errType == "" {
		errType = "invalid_request_error"
	}
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
		"request_id": c.GetString("request_id"),
	})
}
```

- [ ] **Step 3: Update existing tests to assert `request_id`**

In `backend/internal/apiresponse/errors_test.go`, add to `TestAbortOpenAIErrorShape` after the existing assertions (after line 38):

```go
	if _, ok := errObj["request_id"]; !ok {
		t.Error("missing error.request_id")
	}
```

Update `TestAbortAnthropicErrorShape` line 63 from:
```go
	if _, ok := resp["request_id"]; !ok {
```
to:
```go
	if resp["request_id"] == nil {
```

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/apiresponse/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/apiresponse/errors.go backend/internal/apiresponse/errors_test.go
git commit -m "feat(apiresponse): include request_id in OpenAI and Anthropic error responses"
```

---

## Task 5: Fix JWT middleware error format

**Files:**
- Modify: `backend/internal/middleware/jwt_auth.go:14-46`

- [ ] **Step 1: Replace all `{"error": "string"}` with structured shape**

In `backend/internal/middleware/jwt_auth.go`, replace the four error responses:

Line 15:
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
```
→
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "missing Authorization header"}})
```

Line 22:
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization header format"})
```
→
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid Authorization header format"}})
```

Line 32:
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
```
→
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid or expired token"}})
```

Line 39:
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
```
→
```go
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid token claims"}})
```

- [ ] **Step 2: Run tests**

Run: `cd backend && go test ./internal/middleware/ -v`
Expected: PASS (or no tests exist — verify with `go build`)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/middleware/jwt_auth.go
git commit -m "fix(middleware): use structured error shape in JWT auth responses"
```

---

## Task 6: Add SSE error events to stream handlers

**Files:**
- Modify: `backend/internal/proxy/proxy.go:464-466,516-518,555-557`

- [ ] **Step 1: Add error event to `handleStreamResponse`**

In `backend/internal/proxy/proxy.go`, replace the error handling in the read loop (lines 464-466):

```go
	if err != nil {
		break
	}
```

with:

```go
	if err != nil {
		if err != io.EOF {
			requestID := c.GetString("request_id")
			errorPayload := map[string]interface{}{
				"error": map[string]interface{}{
					"type":       "api_error",
					"message":    "upstream stream interrupted",
					"request_id": requestID,
				},
			}
			if errorJSON, e := json.Marshal(errorPayload); e == nil {
				fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errorJSON)
				flusher.Flush()
			}
		}
		break
	}
```

- [ ] **Step 2: Add error event to `handleRawStreamResponse`**

In `backend/internal/proxy/proxy.go`, replace the error handling in the read loop (lines 516-518):

```go
	if err != nil {
		break
	}
```

with:

```go
	if err != nil {
		if err != io.EOF {
			requestID := c.GetString("request_id")
			errorPayload := map[string]interface{}{
				"error": map[string]interface{}{
					"type":       "api_error",
					"message":    "upstream stream interrupted",
					"request_id": requestID,
				},
			}
			if errorJSON, e := json.Marshal(errorPayload); e == nil {
				fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errorJSON)
				flusher.Flush()
			}
		}
		break
	}
```

- [ ] **Step 3: Add error event to `handleMessagesStreamResponse`**

In `backend/internal/proxy/proxy.go`, replace the error handling in the read loop (lines 555-557):

```go
	if err != nil {
		break
	}
```

with:

```go
	if err != nil {
		if err != io.EOF {
			requestID := c.GetString("request_id")
			errorPayload := map[string]interface{}{
				"error": map[string]interface{}{
					"type":       "api_error",
					"message":    "upstream stream interrupted",
					"request_id": requestID,
				},
			}
			if errorJSON, e := json.Marshal(errorPayload); e == nil {
				fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errorJSON)
				flusher.Flush()
			}
		}
		break
	}
```

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/proxy/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/proxy.go
git commit -m "feat(proxy): emit SSE error event on stream interruption"
```

---

## Task 7: Move `uuid` to direct dependency

**Files:**
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

- [ ] **Step 1: Run go mod tidy**

Run: `cd backend && go mod tidy`
Expected: `github.com/google/uuid` moves from indirect to direct in go.mod

- [ ] **Step 2: Verify build**

Run: `cd backend && go build ./...`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `cd backend && go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add backend/go.mod backend/go.sum
git commit -m "chore: move google/uuid to direct dependency"
```

---

## Task 8: Final verification

- [ ] **Step 1: Run full test suite**

Run: `cd backend && go test ./...`
Expected: PASS

- [ ] **Step 2: Run go vet**

Run: `cd backend && go vet ./...`
Expected: PASS

- [ ] **Step 3: Verify the complete flow compiles**

Run: `cd backend && go build -o /dev/null ./cmd/server/`
Expected: PASS
