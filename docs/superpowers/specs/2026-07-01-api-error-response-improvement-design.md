# API Error Response Improvement — Design Spec

Date: 2026-07-01
Status: Approved

## Problem

API error responses in OmniRelay are inconsistent:

1. **Upstream errors pass through raw** — An Anthropic-format error from an upstream provider leaks to an OpenAI-format client (and vice versa), breaking client parsing.
2. **No request correlation ID** — Error responses lack a `request_id`, making it impossible to correlate user-reported errors with server logs.
3. **JWT middleware uses inconsistent format** — Admin endpoints return `{"error": "string"}` while proxy routes use structured `{error: {type, message, ...}}`.
4. **Streaming errors are silent** — When an SSE stream breaks mid-response, clients see a TCP disconnect instead of a structured error event.

## Goals

- All error responses use the client's expected format (OpenAI or Anthropic)
- Every error response includes a `request_id` for traceability
- Admin/middleware errors use a consistent structured format
- Streaming errors emit a final SSE `error` event before close

## Non-Goals

- Changing the OpenAPI/Anthropic wire formats (we match what we already emit)
- Adding retry logic or error recovery
- Changing the `FormatFromContext` detection logic

## Architecture

```
Client
  ↓ request (with optional X-Request-Id header)
  ↓
[ensureRequestID] → sets c "request_id"
  ↓
[JWT / APIKey middleware] → structured error on failure (with request_id)
  ↓
[Proxy handler] → resolveDispatch → proxyJSONRequest → upstream
  ↓ (non-2xx)
[writeUpstreamErrorBody]
  ↓
[parseUpstreamError(providerType)] → normalized upstreamError{type, message, code, param}
  ↓
[reformatError(err, clientFormat, request_id)] → JSON in client's expected shape
  ↓
c.Data(status, "application/json", normalized)
```

For streaming: if proxyJSONRequest fails, the same writeUpstreamErrorBody path applies.
If the stream starts successfully and then breaks, handlers emit a final SSE error event.

## Components

### 1. New file: `backend/internal/proxy/upstream_error.go`

```go
package proxy

type upstreamError struct {
    ErrType string // e.g. "invalid_request_error", "rate_limit_error"
    Message string
    Code    string // e.g. "model_not_found", "rate_limit_exceeded"
    Param   string // e.g. "model"
}

// parseUpstreamError extracts a normalized error from a provider's raw error JSON body.
// Returns (upstreamError, true) if parsing succeeded, (_, false) if the body is not
// a recognized error shape.
func parseUpstreamError(providerType string, body []byte) (upstreamError, bool)

// reformatError serializes a normalized error into the target format's wire shape,
// including the request_id. Returns JSON-encoded bytes.
func reformatError(err upstreamError, targetFormat apiresponse.Format, requestID string) []byte
```

**Provider parsing logic:**

| Provider  | Source path in error JSON                                           | Extraction |
|-----------|----------------------------------------------------------------------|------------|
| OpenAI    | `error.type`, `error.message`, `error.code`, `error.param`          | Direct     |
| Anthropic | `error.type`, `error.message`                                        | Map Anthropic type → normalized type |
| Gemini    | `error.status` (as type), `error.message`, `error.code` (as code)   | Map Gemini `status` → normalized type |
| Unknown   | `error.message` or top-level `message`                              | Fallback   |

**Type mapping (provider → normalized):**

| Provider type      | Normalized         | OpenAI type              | Anthropic type        |
|--------------------|--------------------|--------------------------|-----------------------|
| `invalid_request_*` | `bad_request`     | `invalid_request_error`  | `invalid_request_error` |
| `rate_limit_*`     | `rate_limit`       | `rate_limit_exceeded`    | `rate_limit_error`    |
| `authentication_*` | `auth`             | `invalid_request_error`  | `authentication_error` |
| `not_found`        | `not_found`        | `model_not_found` code   | `not_found_error`     |
| `api_error`        | `server`           | `server_error`           | `api_error`           |
| `overloaded`       | `capacity`         | `server_error`           | `api_error`           |

If mapping doesn't have a precise match, the original provider type string is preserved in the `type` field.

### 2. Modified: `backend/internal/proxy/proxy_helpers.go`

**`writeUpstreamErrorBody`** becomes format-aware:

```go
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

All call sites updated to pass `providerType`.

**New helper: `ensureRequestID`**

```go
func ensureRequestID(c *gin.Context) string {
    if id := c.GetHeader("X-Request-Id"); id != "" {
        c.Set("request_id", id)
        return id
    }
    id := uuid.New().String()
    c.Set("request_id", id)
    return id
}
```

Called at the top of `HandleChatCompletions`, `HandleMessages`, `HandlePathRouted`.

### 3. Modified: `backend/internal/apiresponse/errors.go`

**`abortOpenAI`** adds `request_id`:

```go
func abortOpenAI(c *gin.Context, status int, errType, message, code, param, requestID string) {
    // ... existing logic ...
    if requestID != "" {
        errObj["request_id"] = requestID
    }
    c.JSON(status, gin.H{"error": errObj})
}
```

All `Abort*` convenience functions updated to extract `request_id` from context and pass through.

**`abortAnthropic`** populates `request_id`:

```go
func abortAnthropic(c *gin.Context, status int, errType, message, requestID string) {
    // ... existing logic ...
    c.JSON(status, gin.H{
        "type": "error",
        "error": gin.H{"type": errType, "message": message},
        "request_id": requestID,  // was: nil
    })
}
```

### 4. Modified: `backend/internal/middleware/jwt_auth.go`

Replace plain `{"error": "..."}` responses with structured Anthropic-style shape:

```go
// All JWT errors become:
c.JSON(401, gin.H{
    "error": gin.H{
        "type": "authentication_error",
        "message": "...",
    },
})
```

### 5. Modified: Stream handlers (`proxy.go`)

In `handleStreamResponse`, `handleMessagesStreamResponse`, and `handleRawStreamResponse` — on unexpected EOF, emit a final SSE error event:

```go
if err != nil && err != io.EOF {
    errorPayload := map[string]interface{}{
        "error": map[string]interface{}{
            "type": "api_error",
            "message": "upstream stream interrupted",
            "request_id": c.GetString("request_id"),
        },
    }
    errorJSON, _ := json.Marshal(errorPayload)
    fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errorJSON)
    flusher.Flush()
}
```

### 6. Modified call sites

| File | Change |
|------|--------|
| `proxy_helpers.go:60` | `writeUpstreamErrorBody` signature: add `providerType` param |
| `proxy_helpers.go:96` | Pass `providerType` to `writeUpstreamErrorBody` |
| `proxy.go:647` | Pass `providerType` to `writeUpstreamErrorBody` |
| `proxy.go:16-91` | Add `ensureRequestID(c)` at handler entry points |
| `apiresponse/errors.go` | All `Abort*` functions read `request_id` from context |
| `jwt_auth.go` | Structured error shape |

## Error Response Examples

### OpenAI format (after normalization)
```json
{
  "error": {
    "message": "You exceeded your current quota, please check your plan and billing details.",
    "type": "server_error",
    "param": null,
    "code": "rate_limit_exceeded",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

### Anthropic format (after normalization)
```json
{
  "type": "error",
  "error": {
    "type": "rate_limit_error",
    "message": "You exceeded your current quota, please check your plan and billing details."
  },
  "request_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

## Testing Plan

1. **Unit tests** for `parseUpstreamError` — verify each provider format extracts correctly
2. **Unit tests** for `reformatError` — verify output JSON matches expected wire format
3. **Unit tests** for `writeUpstreamErrorBody` — verify format-aware reformatting
4. **Existing tests** — update `errors_test.go` to include `request_id` assertions
5. **Contract tests** — update `openapi_contract_test.go` to include `request_id` field

## Risks / Trade-offs

- **Upstream error parsing** is best-effort; unrecognized shapes fall through to raw passthrough (safe default)
- **Type mapping** may not cover all provider-specific error types — original string is preserved as fallback
- **SSE error events** after partial stream may confuse some clients, but silent disconnect is worse
- **request_id generation** adds a UUID dependency (`github.com/google/uuid` — lightweight, no CGO)
