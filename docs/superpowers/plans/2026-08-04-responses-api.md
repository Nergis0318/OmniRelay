# `/v1/responses` 엔드포인트 지원 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** OmniRelay에 OpenAI Responses API(`/v1/responses`) 번역 엔드포인트를 추가해 Vercel AI SDK `openai()` 클라이언트가 모든 업스트림에서 동작하게 한다.

**Architecture:** Responses 요청을 `responsesToChatBody()`로 chat completions 형식으로 변환 → 기존 `resolveDispatch` + `buildAndSendChatRequest`(executeChat에서 분리) 파이프라인 재사용 → 응답을 `chatResponseToResponses()`(비스트리밍) 또는 `handleResponsesStream`(SSE)으로 Responses 형식으로 재변환. 사용량 로깅·인증·모델 해석은 전부 기존 코드 재사용.

**Tech Stack:** Go 1.25, Gin, 기존 어댑터(`OpenAIAdapter` 등). 새 의존성 없음.

## Global Constraints

- 스펙: `docs/superpowers/specs/2026-08-04-responses-api-design.md`
- Go: `gofmt` 탭, 파일명 = 패키지명 소문자
- 새 외부 의존성 추가 금지 (`crypto/rand`, `encoding/hex` 표준 라이브러리 사용)
- 기존 동작 불변: Task 3·4 리팩터링 후 기존 테스트 전부 통과해야 함
- 모든 태스크 끝에 `go vet ./...` + `go test ./...` 실행 (backend/ 디렉터리에서)
- 커밋은 태스크 단위로

---

### Task 1: 요청 변환 — `ValidateResponsesBody` + `responsesToChatBody`

**Files:**
- Modify: `backend/internal/apiresponse/validation.go`
- Create: `backend/internal/proxy/responses.go` (이 태스크에서는 `responsesToChatBody` + 헬퍼만)
- Create: `backend/internal/apiresponse/validation_test.go`
- Create: `backend/internal/proxy/responses_test.go` (이 태스크에서는 요청 변환 테스트만)

**Interfaces:**
- Consumes: 없음 (기존 `stripProviderPrefix`는 어댑터가 처리하므로 여기서는 사용 안 함)
- Produces:
  - `func ValidateResponsesBody(body map[string]interface{}) (param string, err error)` — apiresponse 패키지
  - `func responsesToChatBody(body map[string]interface{}) (map[string]interface{}, error)` — proxy 패키지, 순수 함수. Task 6의 `HandleResponses`가 사용.
  - 내부 헬퍼: `inputToMessages(input interface{}) ([]map[string]interface{}, error)`, `convertResponsesContent(content interface{}) interface{}`, `convertResponsesTools(tools []interface{}) []map[string]interface{}`

- [ ] **Step 1: `ValidateResponsesBody` 테스트 작성**

`backend/internal/apiresponse/validation_test.go`:

```go
package apiresponse

import "testing"

func TestValidateResponsesBody(t *testing.T) {
	if param, err := ValidateResponsesBody(map[string]interface{}{"input": "hi"}); err == nil {
		t.Errorf("expected model error, got nil (param=%s)", param)
	}
	if param, err := ValidateResponsesBody(map[string]interface{}{"model": "gpt-4o"}); err == nil {
		t.Errorf("expected input error, got nil (param=%s)", param)
	}
	if _, err := ValidateResponsesBody(map[string]interface{}{"model": "gpt-4o", "input": "hi"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if _, err := ValidateResponsesBody(map[string]interface{}{"model": "gpt-4o", "input": []interface{}{}}); err == nil {
		t.Errorf("expected empty input error")
	}
	if _, err := ValidateResponsesBody(map[string]interface{}{"model": "gpt-4o", "input": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: 테스트가 실패하는지 확인**

Run: `go test ./internal/apiresponse/ -run TestValidateResponsesBody -v`
Expected: FAIL (컴파일 에러 — 함수 없음)

- [ ] **Step 3: `ValidateResponsesBody` 구현**

`backend/internal/apiresponse/validation.go`에 추가:

```go
// ValidateResponsesBody checks OpenAI CreateResponseRequest required fields.
func ValidateResponsesBody(body map[string]interface{}) (param string, err error) {
	if _, ok := body["model"].(string); !ok {
		return "model", fmt.Errorf("you must provide a model parameter")
	}
	input, ok := body["input"]
	if !ok {
		return "input", fmt.Errorf("you must provide an input parameter")
	}
	if _, isString := input.(string); isString {
		return "", nil
	}
	items, isArray := input.([]interface{})
	if !isArray || len(items) == 0 {
		return "input", fmt.Errorf("input must be a string or a non-empty array")
	}
	return "", nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `go test ./internal/apiresponse/ -run TestValidateResponsesBody -v`
Expected: PASS

- [ ] **Step 5: `responsesToChatBody` 테스트 작성**

`backend/internal/proxy/responses_test.go`:

```go
package proxy

import (
	"testing"
)

func TestResponsesToChatBodyStringInput(t *testing.T) {
	body := map[string]interface{}{
		"model":             "openai/gpt-4o",
		"input":             "hello",
		"instructions":      "be nice",
		"max_output_tokens": 100,
		"temperature":       0.5,
		"stream":            true,
	}
	chat, err := responsesToChatBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat["model"] != "openai/gpt-4o" {
		t.Errorf("model = %v", chat["model"])
	}
	if chat["max_tokens"] != float64(100) {
		t.Errorf("max_tokens = %v", chat["max_tokens"])
	}
	if chat["temperature"] != 0.5 || chat["stream"] != true {
		t.Errorf("passthrough fields = %#v", chat)
	}
	messages := chat["messages"].([]map[string]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages len = %d", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "be nice" {
		t.Errorf("first message = %#v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "hello" {
		t.Errorf("second message = %#v", messages[1])
	}
}

func TestResponsesToChatBodyInputArray(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "what's the weather?"},
		}},
		map[string]interface{}{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": map[string]interface{}{"city": "seoul"}},
		map[string]interface{}{"type": "function_call_output", "call_id": "call_1", "output": "sunny"},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": input})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := chat["messages"].([]map[string]interface{})
	if len(messages) != 3 {
		t.Fatalf("messages len = %d", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Errorf("messages[0] role = %v", messages[0]["role"])
	}
	parts := messages[0]["content"].([]map[string]interface{})
	if parts[0]["type"] != "text" || parts[0]["text"] != "what's the weather?" {
		t.Errorf("messages[0] content = %#v", messages[0]["content"])
	}
	if messages[1]["role"] != "assistant" {
		t.Errorf("messages[1] role = %v", messages[1]["role"])
	}
	toolCalls := messages[1]["tool_calls"].([]map[string]interface{})
	fn := toolCalls[0]["function"].(map[string]interface{})
	if fn["name"] != "get_weather" || fn["arguments"] != `{"city":"seoul"}` {
		t.Errorf("tool call = %#v", toolCalls[0])
	}
	if messages[2]["role"] != "tool" || messages[2]["tool_call_id"] != "call_1" || messages[2]["content"] != "sunny" {
		t.Errorf("messages[2] = %#v", messages[2])
	}
}

func TestResponsesToChatBodyTools(t *testing.T) {
	tools := []interface{}{
		map[string]interface{}{"type": "function", "name": "get_weather", "description": "d", "strict": true, "parameters": map[string]interface{}{"type": "object"}},
		map[string]interface{}{"type": "web_search"},
	}
	chat, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": "hi", "tools": tools})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	converted := chat["tools"].([]map[string]interface{})
	if len(converted) != 1 {
		t.Fatalf("tools len = %d (web_search should be dropped)", len(converted))
	}
	fn := converted[0]["function"].(map[string]interface{})
	if converted[0]["type"] != "function" || fn["name"] != "get_weather" || fn["strict"] != true {
		t.Errorf("converted tool = %#v", converted[0])
	}
}

func TestResponsesToChatBodyInvalidInput(t *testing.T) {
	if _, err := responsesToChatBody(map[string]interface{}{"model": "openai/gpt-4o", "input": 42}); err == nil {
		t.Errorf("expected error for numeric input")
	}
}
```

- [ ] **Step 6: 테스트가 실패하는지 확인**

Run: `go test ./internal/proxy/ -run 'TestResponsesToChatBody' -v`
Expected: FAIL (컴파일 에러 — 함수 없음)

- [ ] **Step 7: `responsesToChatBody` 구현**

`backend/internal/proxy/responses.go` 생성:

```go
package proxy

import (
	"encoding/json"
	"fmt"
)

// responsesToChatBody converts an OpenAI Responses API request body into a
// chat completions request body for the existing proxy pipeline.
func responsesToChatBody(body map[string]interface{}) (map[string]interface{}, error) {
	chat := make(map[string]interface{})

	for _, key := range []string{"model", "stream", "temperature", "top_p", "stop"} {
		if v, ok := body[key]; ok {
			chat[key] = v
		}
	}
	if v, ok := body["max_output_tokens"]; ok {
		chat["max_tokens"] = v
	}

	messages, err := inputToMessages(body["input"])
	if err != nil {
		return nil, err
	}
	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages = append([]map[string]interface{}{{"role": "system", "content": instructions}}, messages...)
	}
	chat["messages"] = messages

	if tools, ok := body["tools"].([]interface{}); ok {
		chat["tools"] = convertResponsesTools(tools)
	}
	return chat, nil
}

func inputToMessages(input interface{}) ([]map[string]interface{}, error) {
	if s, ok := input.(string); ok {
		return []map[string]interface{}{{"role": "user", "content": s}}, nil
	}
	items, ok := input.([]interface{})
	if !ok {
		return nil, fmt.Errorf("input must be a string or an array")
	}
	var messages []map[string]interface{}
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if msgType, _ := item["type"].(string); msgType != "" && msgType != "message" {
			switch msgType {
			case "function_call_output":
				messages = append(messages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": item["call_id"],
					"content":      item["output"],
				})
			case "function_call":
				args := "{}"
				if a, ok := item["arguments"]; ok {
					if b, err := json.Marshal(a); err == nil {
						args = string(b)
					}
				}
				messages = append(messages, map[string]interface{}{
					"role": "assistant",
					"tool_calls": []map[string]interface{}{{
						"id":   item["call_id"],
						"type": "function",
						"function": map[string]interface{}{
							"name":      item["name"],
							"arguments": args,
						},
					}},
				})
			}
			continue
		}
		role, _ := item["role"].(string)
		if role == "" {
			role = "user"
		}
		messages = append(messages, map[string]interface{}{
			"role":    role,
			"content": convertResponsesContent(item["content"]),
		})
	}
	return messages, nil
}

func convertResponsesContent(content interface{}) interface{} {
	if s, ok := content.(string); ok {
		return s
	}
	parts, ok := content.([]interface{})
	if !ok {
		return ""
	}
	out := make([]map[string]interface{}, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch part["type"] {
		case "input_text":
			out = append(out, map[string]interface{}{"type": "text", "text": part["text"]})
		case "input_image":
			out = append(out, map[string]interface{}{"type": "image_url", "image_url": part["image_url"]})
		default:
			out = append(out, part)
		}
	}
	return out
}

func convertResponsesTools(tools []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]interface{})
		if !ok || tool["type"] != "function" {
			continue
		}
		fn := make(map[string]interface{})
		for _, k := range []string{"name", "description", "parameters", "strict"} {
			if v, ok := tool[k]; ok {
				fn[k] = v
			}
		}
		out = append(out, map[string]interface{}{"type": "function", "function": fn})
	}
	return out
}
```

- [ ] **Step 8: 테스트 통과 확인 + vet**

Run: `go test ./internal/proxy/ -run 'TestResponsesToChatBody' -v && go vet ./internal/proxy/`
Expected: PASS

- [ ] **Step 9: 커밋**

```bash
git add backend/internal/apiresponse/validation.go backend/internal/apiresponse/validation_test.go backend/internal/proxy/responses.go backend/internal/proxy/responses_test.go
git commit -m "feat: responses request -> chat completions conversion"
```

---

### Task 2: 비스트리밍 응답 변환 — `chatResponseToResponses` + `randomID`

**Files:**
- Modify: `backend/internal/proxy/responses.go` (이 태스크에서 `chatResponseToResponses` + `randomID` 추가)
- Modify: `backend/internal/proxy/responses_test.go` (응답 변환 테스트 추가)

**Interfaces:**
- Consumes: `numberToInt64` (proxy 패키지 기존 함수)
- Produces:
  - `func randomID(prefix string) string` — `resp_`/`msg_`/`fc_`/`call_` 프리픽스 ID 생성. Task 5에서도 사용.
  - `func chatResponseToResponses(chatResp map[string]interface{}, fullModelID string) map[string]interface{}` — chat completions 응답 map → Responses 응답 map. Task 6에서 사용.

- [ ] **Step 1: 테스트 작성**

`backend/internal/proxy/responses_test.go`에 추가:

```go
func TestChatResponseToResponses(t *testing.T) {
	chat := map[string]interface{}{
		"id": "cmpl-1",
		"choices": []interface{}{
			map[string]interface{}{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "hello",
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":   "call_9",
							"type": "function",
							"function": map[string]interface{}{"name": "get_weather", "arguments": `{"city":"seoul"}`},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]interface{}{"prompt_tokens": 4, "completion_tokens": 3, "total_tokens": 7},
	}
	resp := chatResponseToResponses(chat, "openai/gpt-4o")

	if resp["object"] != "response" || resp["model"] != "openai/gpt-4o" || resp["status"] != "completed" {
		t.Errorf("base fields = %#v", resp)
	}
	if resp["output_text"] != "hello" {
		t.Errorf("output_text = %v", resp["output_text"])
	}
	output := resp["output"].([]map[string]interface{})
	if len(output) != 2 {
		t.Fatalf("output len = %d", len(output))
	}
	if output[0]["type"] != "message" || output[1]["type"] != "function_call" {
		t.Errorf("output types = %v, %v", output[0]["type"], output[1]["type"])
	}
	fc := output[1]
	if fc["call_id"] != "call_9" || fc["name"] != "get_weather" || fc["arguments"] != `{"city":"seoul"}` {
		t.Errorf("function_call = %#v", fc)
	}
	usage := resp["usage"].(map[string]interface{})
	if usage["input_tokens"] != int64(4) || usage["output_tokens"] != int64(3) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestChatResponseToResponsesIncomplete(t *testing.T) {
	chat := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{
			"message":       map[string]interface{}{"role": "assistant", "content": "part"},
			"finish_reason": "length",
		}},
	}
	resp := chatResponseToResponses(chat, "openai/gpt-4o")
	if resp["status"] != "incomplete" {
		t.Errorf("status = %v", resp["status"])
	}
}
```

- [ ] **Step 2: 테스트가 실패하는지 확인**

Run: `go test ./internal/proxy/ -run 'TestChatResponseToResponses' -v`
Expected: FAIL (컴파일 에러 — 함수 없음)

- [ ] **Step 3: 구현**

`backend/internal/proxy/responses.go`에 추가 (import에 `crypto/rand`, `encoding/hex`, `strings`, `time` 추가):

```go
func randomID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return prefix + "000000000000"
	}
	return prefix + hex.EncodeToString(b)
}

// chatResponseToResponses converts a chat completions response map into the
// OpenAI Responses API response shape.
func chatResponseToResponses(chatResp map[string]interface{}, fullModelID string) map[string]interface{} {
	var output []map[string]interface{}
	var outputText strings.Builder

	choices, _ := chatResp["choices"].([]interface{})
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})

		if content, ok := msg["content"].(string); ok && content != "" {
			outputText.WriteString(content)
			output = append(output, map[string]interface{}{
				"id":     randomID("msg_"),
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": content, "annotations": []interface{}{}},
				},
			})
		}

		if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
			for _, raw := range toolCalls {
				tc, _ := raw.(map[string]interface{})
				fn, _ := tc["function"].(map[string]interface{})
				callID, _ := tc["id"].(string)
				if callID == "" {
					callID = randomID("call_")
				}
				output = append(output, map[string]interface{}{
					"id":        randomID("fc_"),
					"type":      "function_call",
					"call_id":   callID,
					"name":      fn["name"],
					"arguments": fn["arguments"],
				})
			}
		}
	}

	status := "completed"
	if len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if fr, _ := choice["finish_reason"].(string); fr == "length" {
				status = "incomplete"
			}
		}
	}

	usage := map[string]interface{}{}
	if u, ok := chatResp["usage"].(map[string]interface{}); ok {
		inTok := numberToInt64(u["prompt_tokens"])
		outTok := numberToInt64(u["completion_tokens"])
		cached := int64(0)
		if details, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
			cached = numberToInt64(details["cached_tokens"])
		}
		usage = map[string]interface{}{
			"input_tokens":  inTok,
			"output_tokens": outTok,
			"total_tokens":  inTok + outTok,
			"input_tokens_details": map[string]interface{}{
				"cached_tokens": cached,
			},
			"output_tokens_details": map[string]interface{}{
				"reasoning_tokens": int64(0),
			},
		}
	}

	return map[string]interface{}{
		"id":          randomID("resp_"),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      status,
		"model":       fullModelID,
		"output":      output,
		"output_text": outputText.String(),
		"usage":       usage,
	}
}
```

- [ ] **Step 4: 테스트 통과 확인 + vet**

Run: `go test ./internal/proxy/ -run 'TestChatResponseToResponses' -v && go vet ./internal/proxy/`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add backend/internal/proxy/responses.go backend/internal/proxy/responses_test.go
git commit -m "feat: chat completions response -> responses format conversion"
```

---

### Task 3: 리팩터링 — `handleNonStreamChatResponse` → `parseNonStreamChatResponse`

**Files:**
- Modify: `backend/internal/proxy/upstream.go:101-163`

**Interfaces:**
- Consumes: 기존 `handleNonStreamChatResponse` 본문 전체
- Produces:
  - `func parseNonStreamChatResponse(c *gin.Context, respBody []byte, respHeader http.Header, adapter Adapter, fullModelID string, dbModel *models.Model, apiKeyID, providerID int64, startTime time.Time, userID int64, usageService UsageLogger, providerType string, inputTokens int64) (map[string]interface{}, bool)` — chat 응답 map과 "이미 c에 에러를 썼는지" bool 반환. Task 6에서 사용.
  - `handleNonStreamChatResponse`는 얇은 래퍼로 유지 — 호출부(executeChat) 변경 없음.

- [ ] **Step 1: 리팩터링**

`backend/internal/proxy/upstream.go:101`의 함수를 아래로 교체:

```go
func handleNonStreamChatResponse(c *gin.Context, respBody []byte, respHeader http.Header, adapter Adapter, fullModelID string, dbModel *models.Model, apiKeyID, providerID int64, startTime time.Time, userID int64, usageService UsageLogger, providerType string, inputTokens int64) {
	finalResponse, wrote := parseNonStreamChatResponse(respBody, respHeader, adapter, fullModelID, dbModel, apiKeyID, providerID, startTime, userID, usageService, providerType, inputTokens)
	if !wrote && finalResponse != nil {
		c.JSON(http.StatusOK, finalResponse)
	}
}

// parseNonStreamChatResponse parses an upstream chat completions response,
// logs usage, and returns the final chat-format response map. The bool is
// true when an error response was already written to c (nothing to send).
func parseNonStreamChatResponse(c *gin.Context, respBody []byte, respHeader http.Header, adapter Adapter, fullModelID string, dbModel *models.Model, apiKeyID, providerID int64, startTime time.Time, userID int64, usageService UsageLogger, providerType string, inputTokens int64) (map[string]interface{}, bool) {
	var modelResponse map[string]interface{}
	if err := json.Unmarshal(respBody, &modelResponse); err != nil {
		c.Data(http.StatusOK, contentTypeOrDefault(respHeader), respBody)
		return nil, true
	}

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

	// Extract upstream token data for cache tokens which we can't count locally
	var cacheWrite5m, cacheWrite1h, cacheReadTokens int64
	var upstreamReqTokens, upstreamRespTokens int64
	if usage, ok := finalResponse["usage"].(map[string]interface{}); ok {
		upstreamReqTokens = numberToInt64(usage["prompt_tokens"])
		upstreamRespTokens = numberToInt64(usage["completion_tokens"])
		cacheWrite5m, cacheWrite1h, cacheReadTokens = extractCacheTokens(usage)
	}

	// Prefer locally counted tokens over upstream values
	reqTokens := inputTokens
	if reqTokens == 0 {
		reqTokens = upstreamReqTokens
	}
	respTokens := localOutput
	if respTokens == 0 {
		respTokens = upstreamRespTokens
	}
	totalTokens := reqTokens + respTokens

	if reqTokens > 0 || respTokens > 0 || cacheWrite5m > 0 || cacheWrite1h > 0 || cacheReadTokens > 0 {
		cost := calculateCost(dbModel, reqTokens, respTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)

		usageService.Log(models.UsageLog{
			APIKeyID:           &apiKeyID,
			ProviderID:         &providerID,
			Model:              fullModelID,
			RequestTokens:      reqTokens,
			ResponseTokens:     respTokens,
			TotalTokens:        totalTokens,
			CacheWrite5MTokens: cacheWrite5m,
			CacheWrite1HTokens: cacheWrite1h,
			CacheReadTokens:    cacheReadTokens,
			LatencyMs:          latencyMs,
			Cost:               cost,
			StartedAt:          &startTime,
			CompletedAt:        &completedAt,
			UserID:             &userID,
		})
	}

	return finalResponse, false
}
```

**주의:** 기존 `handleNonStreamChatResponse` 본문에서 `c.JSON(http.StatusOK, finalResponse)` 마지막 줄만 `return finalResponse, false`로 바꾸고, `c.Data(...); return` 두 곳은 `return nil, true`로 바꾼 형태다. `c *gin.Context`가 첫 파라미터로 필요하며(내부에서 `c.Data` 호출), 반환 값의 bool은 "이미 c에 에러 응답을 썼는지"를 뜻한다. Task 6 호출부가 `parseNonStreamChatResponse(c, respBody, ...)`로 호출한다.

- [ ] **Step 2: 기존 테스트로 동작 확인**

Run: `go test ./internal/proxy/ -run 'TestExecuteChat|TestHandleNonStream|TestUsage|TestNonStream' -v`
(실패 없이 전체 스위트 실행) — 이어서:

Run: `go test ./... && go vet ./...`
Expected: 전부 PASS (동작 불변)

- [ ] **Step 3: 커밋**

```bash
git add backend/internal/proxy/upstream.go
git commit -m "refactor: split handleNonStreamChatResponse into parseNonStreamChatResponse"
```

---

### Task 4: 리팩터링 — `executeChat` → `buildAndSendChatRequest`

**Files:**
- Modify: `backend/internal/proxy/chat_handler.go:13-79`

**Interfaces:**
- Consumes: 기존 `executeChat` 본문
- Produces:
  - `func (e *Engine) buildAndSendChatRequest(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID, userID int64) (*http.Response, time.Time, int64, bool)` — (업스트림 응답, 시작 시각, 로컬 input 토큰 수, 에러를 이미 c에 썼는지). Task 6에서 사용.
  - `executeChat`는 기존 동작 그대로 유지(내부에서 buildAndSendChatRequest 호출).

- [ ] **Step 1: 리팩터링**

`backend/internal/proxy/chat_handler.go`의 `executeChat`를 아래로 교체:

```go
func (e *Engine) executeChat(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID, userID int64) {
	resp, startTime, inputTokens, wroteError := e.buildAndSendChatRequest(c, provider, dbModel, adapter, body, fullModelID, apiKeyID, userID)
	if wroteError {
		return
	}
	defer resp.Body.Close()

	if extractStreamFlag(body) {
		e.handleStreamResponse(c, resp, adapter, apiKeyID, provider.ID, fullModelID, dbModel, userID, provider.ProviderType, inputTokens)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: "failed to read the model response",
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read the model response"})
		return
	}

	handleNonStreamChatResponse(c, respBody, resp.Header, adapter, fullModelID, dbModel, apiKeyID, provider.ID, startTime, userID, e.usageService, provider.ProviderType, inputTokens)
}

// buildAndSendChatRequest builds the upstream chat request, sends it, and
// returns the upstream response. wroteError is true when an error response
// was already written to c.
func (e *Engine) buildAndSendChatRequest(c *gin.Context, provider *models.Provider, dbModel *models.Model, adapter Adapter, body map[string]interface{}, fullModelID string, apiKeyID, userID int64) (*http.Response, time.Time, int64, bool) {
	isStream := extractStreamFlag(body)

	if dbModel.ContextWindow > 0 {
		body["_context_window"] = dbModel.ContextWindow
	}

	endpoint, adaptedBody, err := adapter.BuildChatRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, time.Time{}, 0, true
	}

	// Inject stream_options for OpenAI-compatible streaming to get usage data
	if isStream && isOpenAICompat(provider.ProviderType) {
		if _, ok := adaptedBody["stream_options"]; !ok {
			adaptedBody["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}

	// Count input tokens locally before sending to upstream
	inputTokens := countInputTokens(adaptedBody, fullModelID)

	rc := &requestContext{
		c:           c,
		provider:    provider,
		dbModel:     dbModel,
		apiKeyID:    apiKeyID,
		userID:      userID,
		fullModelID: fullModelID,
		engine:      e,
	}

	resp, startTime, wroteError := rc.executeUpstream(adaptedBody, endpoint, isStream)
	if wroteError {
		return nil, time.Time{}, 0, true
	}

	if !isSuccessStatus(resp.StatusCode) {
		latencyMs := time.Since(startTime).Milliseconds()
		logErrorResponse(e, apiKeyID, provider.ID, fullModelID, resp.StatusCode, latencyMs, userID)
		writeUpstreamErrorBody(c, resp, provider.ProviderType)
		return nil, time.Time{}, 0, true
	}

	return resp, startTime, inputTokens, false
}
```

- [ ] **Step 2: 기존 테스트로 동작 확인**

Run: `go test ./... && go vet ./...`
Expected: 전부 PASS (동작 불변)

- [ ] **Step 3: 커밋**

```bash
git add backend/internal/proxy/chat_handler.go
git commit -m "refactor: split buildAndSendChatRequest out of executeChat"
```

---

### Task 5: 스트리밍 변환 — `handleResponsesStream`

**Files:**
- Create: `backend/internal/proxy/responses_stream.go`
- Modify: `backend/internal/proxy/responses_test.go` (스트리밍 테스트 추가)

**Interfaces:**
- Consumes: `randomID` (Task 2), `newStreamWriter`/`startKeepAlive`/`copyResponseHeaders` (기존 stream.go), `adapter.ParseStreamChunk` (기존), `numberToInt64`, `countTextTokens`, `calculateCost`, `extractStreamFlag` (기존), `isSuccessStatus`(기존, 로직상 핸들러가 이미 성공 응답만 받음)
- Produces:
  - `func (e *Engine) handleResponsesStream(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, userID int64, providerType string, inputTokens int64)` — Task 6에서 사용.

- [ ] **Step 1: 테스트 작성**

`backend/internal/proxy/responses_test.go`에 추가 (import에 `io`, `net/http`, `net/http/httptest`, `strings`, `path/filepath`, `omnirelay/internal/database`, `omnirelay/internal/service` 추가):

```go
func TestHandleResponsesStreamText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n" +
		"data: [DONE]\n\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleResponsesStream(c, upstream, &OpenAIAdapter{}, 1, 1, "openai/gpt-4o", nil, 1, "openai", 5)

	body := w.Body.String()
	for _, want := range []string{
		`"type":"response.created"`,
		`"type":"response.output_text.delta"`,
		`"delta":"Hel"`,
		`"delta":"lo"`,
		`"type":"response.output_text.done"`,
		`"type":"response.output_item.done"`,
		`"type":"response.completed"`,
		`"status":"completed"`,
		`"output_text":"Hello"`,
		`"input_tokens":5`,
		`"output_tokens":3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %s\nbody: %s", want, body)
		}
	}
}

func TestHandleResponsesStreamToolCall(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"seoul\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(nil, nil, usageSvc, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	engine.handleResponsesStream(c, upstream, &OpenAIAdapter{}, 1, 1, "openai/gpt-4o", nil, 1, "openai", 0)

	body := w.Body.String()
	for _, want := range []string{
		`"type":"function_call"`,
		`"type":"response.function_call_arguments.delta"`,
		`"type":"response.function_call_arguments.done"`,
		`"arguments":"{\"city\":\"seoul\"}"`,
		`"call_id":"call_1"`,
		`"name":"get_weather"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %s\nbody: %s", want, body)
		}
	}
}
```

- [ ] **Step 2: 테스트가 실패하는지 확인**

Run: `go test ./internal/proxy/ -run 'TestHandleResponsesStream' -v`
Expected: FAIL (컴파일 에러 — 함수 없음)

- [ ] **Step 3: 구현**

`backend/internal/proxy/responses_stream.go` 생성:

```go
package proxy

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

// handleResponsesStream translates an upstream chat-completions SSE stream
// into OpenAI Responses API SSE events.
func (e *Engine) handleResponsesStream(c *gin.Context, resp *http.Response, adapter Adapter, apiKeyID, providerID int64, fullModelID string, dbModel *models.Model, userID int64, providerType string, inputTokens int64) {
	c.Status(http.StatusOK)
	copyResponseHeaders(c, resp.Header)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	done := make(chan struct{})
	defer close(done)
	sw := newStreamWriter(c.Writer, flusher)
	startKeepAlive(sw, done)

	start := time.Now()
	state := make(map[string]interface{})
	responseID := randomID("resp_")

	emit := func(ev map[string]interface{}) {
		b, _ := json.Marshal(ev)
		sw.Write([]byte("data: " + string(b) + "\n\n"))
	}

	emit(map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"model":  fullModelID,
			"output": []interface{}{},
		},
	})

	type item struct {
		kind      string // "message" | "function_call"
		id        string
		callID    string
		name      string
		toolIndex int
		text      strings.Builder
		args      strings.Builder
	}

	var current *item
	var outputItems []interface{}
	var outputTextAccum strings.Builder

	closeItem := func() {
		if current == nil {
			return
		}
		idx := len(outputItems)
		switch current.kind {
		case "message":
			text := current.text.String()
			itemObj := map[string]interface{}{
				"id": current.id, "type": "message", "status": "completed", "role": "assistant",
				"content": []map[string]interface{}{{"type": "output_text", "text": text, "annotations": []interface{}{}}},
			}
			emit(map[string]interface{}{"type": "response.output_text.done", "item_id": current.id, "output_index": idx, "content_index": 0, "text": text})
			emit(map[string]interface{}{"type": "response.content_part.done", "item_id": current.id, "output_index": idx, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": text, "annotations": []interface{}{}}})
			emit(map[string]interface{}{"type": "response.output_item.done", "output_index": idx, "item": itemObj})
			outputItems = append(outputItems, itemObj)
		case "function_call":
			args := current.args.String()
			itemObj := map[string]interface{}{
				"id": current.id, "type": "function_call", "call_id": current.callID,
				"name": current.name, "arguments": args,
			}
			emit(map[string]interface{}{"type": "response.function_call_arguments.done", "item_id": current.id, "output_index": idx, "arguments": args})
			emit(map[string]interface{}{"type": "response.output_item.done", "output_index": idx, "item": itemObj})
			outputItems = append(outputItems, itemObj)
		}
		current = nil
	}

	openMessage := func() {
		itemID := randomID("msg_")
		current = &item{kind: "message", id: itemID}
		idx := len(outputItems)
		emit(map[string]interface{}{"type": "response.output_item.added", "output_index": idx, "item": map[string]interface{}{
			"id": itemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []interface{}{},
		}})
		emit(map[string]interface{}{"type": "response.content_part.added", "item_id": itemID, "output_index": idx, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": "", "annotations": []interface{}{}}})
	}

	openFunctionCall := func(callID, name string, index int) {
		itemID := randomID("fc_")
		current = &item{kind: "function_call", id: itemID, callID: callID, name: name, toolIndex: index}
		idx := len(outputItems)
		emit(map[string]interface{}{"type": "response.output_item.added", "output_index": idx, "item": map[string]interface{}{
			"id": itemID, "type": "function_call", "call_id": callID, "name": name, "arguments": "",
		}})
	}

	finishReason := ""
	buf := make([]byte, 4096)
	var totalInputTokens, totalOutputTokens int64
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			_, inTok, outTok, _ := adapter.ParseStreamChunk(chunk, state)
			if inTok > 0 {
				totalInputTokens = inTok
			}
			if outTok > 0 {
				totalOutputTokens = outTok
			}

			scanner := bufio.NewScanner(strings.NewReader(string(chunk)))
			for scanner.Scan() {
				line := scanner.Text()
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				payload := strings.TrimPrefix(line, "data: ")
				if payload == "[DONE]" {
					continue
				}
				var chunkJSON map[string]interface{}
				if err := json.Unmarshal([]byte(payload), &chunkJSON); err != nil {
					continue
				}
				choices, _ := chunkJSON["choices"].([]interface{})
				for _, rawChoice := range choices {
					choice, _ := rawChoice.(map[string]interface{})
					delta, _ := choice["delta"].(map[string]interface{})

					if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
						for _, rawTC := range toolCalls {
							tc, _ := rawTC.(map[string]interface{})
							tcIndex := int(numberToInt64(tc["index"]))
							fn, _ := tc["function"].(map[string]interface{})
							tcName, _ := fn["name"].(string)
							tcArgs, _ := fn["arguments"].(string)

							if current == nil || current.kind != "function_call" || current.toolIndex != tcIndex {
								closeItem()
								tcID, _ := tc["id"].(string)
								openFunctionCall(tcID, tcName, tcIndex)
							}
							if tcArgs != "" {
								current.args.WriteString(tcArgs)
								emit(map[string]interface{}{"type": "response.function_call_arguments.delta", "item_id": current.id, "output_index": len(outputItems), "delta": tcArgs})
							}
						}
						continue
					}

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
				}
			}
		}
		if err != nil {
			break
		}
	}

	closeItem()

	if totalOutputTokens == 0 && outputTextAccum.Len() > 0 {
		totalOutputTokens = countTextTokens(outputTextAccum.String(), fullModelID)
	}
	if inputTokens > 0 {
		totalInputTokens = inputTokens
	}

	cacheWrite5m, _ := state["cache_write_5m_tokens"].(int64)
	cacheWrite1h, _ := state["cache_write_1h_tokens"].(int64)
	cacheReadTokens, _ := state["cache_read_tokens"].(int64)

	status := "completed"
	eventType := "response.completed"
	if finishReason == "length" {
		status = "incomplete"
		eventType = "response.incomplete"
	}

	emit(map[string]interface{}{
		"type": eventType,
		"response": map[string]interface{}{
			"id":          responseID,
			"object":      "response",
			"created_at":  time.Now().Unix(),
			"status":      status,
			"model":       fullModelID,
			"output":      outputItems,
			"output_text": outputTextAccum.String(),
			"usage": map[string]interface{}{
				"input_tokens":  totalInputTokens,
				"output_tokens": totalOutputTokens,
				"total_tokens":  totalInputTokens + totalOutputTokens,
				"input_tokens_details": map[string]interface{}{
					"cached_tokens": cacheReadTokens,
				},
				"output_tokens_details": map[string]interface{}{
					"reasoning_tokens": int64(0),
				},
			},
		},
	})

	latencyMs := time.Since(start).Milliseconds()
	completedAt := time.Now()
	var cost float64
	if dbModel != nil && (totalInputTokens > 0 || totalOutputTokens > 0) {
		cost = calculateCost(dbModel, totalInputTokens, totalOutputTokens, cacheWrite5m, cacheWrite1h, cacheReadTokens)
	}
	e.usageService.Log(models.UsageLog{
		APIKeyID:           &apiKeyID,
		ProviderID:         &providerID,
		Model:              fullModelID,
		RequestTokens:      totalInputTokens,
		ResponseTokens:     totalOutputTokens,
		TotalTokens:        totalInputTokens + totalOutputTokens,
		CacheWrite5MTokens: cacheWrite5m,
		CacheWrite1HTokens: cacheWrite1h,
		CacheReadTokens:    cacheReadTokens,
		LatencyMs:          latencyMs,
		Cost:               cost,
		StartedAt:          &start,
		CompletedAt:        &completedAt,
		UserID:             &userID,
	})
}
```

- [ ] **Step 4: 테스트 통과 확인 + vet**

Run: `go test ./internal/proxy/ -run 'TestHandleResponsesStream' -v && go vet ./internal/proxy/`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add backend/internal/proxy/responses_stream.go backend/internal/proxy/responses_test.go
git commit -m "feat: responses API streaming SSE translation"
```

---

### Task 6: 핸들러 + 라우트 + E2E 테스트

**Files:**
- Modify: `backend/internal/proxy/responses.go` (`HandleResponses` 추가)
- Modify: `backend/cmd/server/main.go` (라우트 추가)
- Modify: `backend/internal/proxy/responses_test.go` (E2E 테스트 추가)

**Interfaces:**
- Consumes: Task 1의 `responsesToChatBody`/`ValidateResponsesBody`, Task 2의 `chatResponseToResponses`, Task 3의 `parseNonStreamChatResponse`, Task 4의 `buildAndSendChatRequest`, Task 5의 `handleResponsesStream`, 기존 `readJSONBody`/`resolveDispatch`/`extractStreamFlag`
- Produces:
  - `func (e *Engine) HandleResponses(c *gin.Context)` — main.go에서 `v1.POST("/responses", proxyEngine.HandleResponses)`로 등록

- [ ] **Step 1: E2E 테스트 작성**

`backend/internal/proxy/responses_test.go`에 추가 (import에 `io`, `net/http`, `net/http/httptest`, `strings`, `path/filepath`, `encoding/json`, `omnirelay/internal/config`, `omnirelay/internal/crypto`, `omnirelay/internal/database`, `omnirelay/internal/models`, `omnirelay/internal/service`, `github.com/gin-gonic/gin` 추가):

```go
func newResponsesTestRouter(t *testing.T, upstream *httptest.Server) (*gin.Engine, *Engine) {
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
		 VALUES (1, 'openai', 'OpenAI', ?, ?, 'openai', 1)`,
		upstream.URL, encryptTestAPIKey(t, "sk-test"),
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'gpt-4o', 'openai', 1, 1)`,
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
	r.POST("/v1/responses", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleResponses(c)
	})
	return r, engine
}

func TestHandleResponsesNonStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"cmpl-1",
			"object":"chat.completion",
			"choices":[{"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
		}`)
	}))
	t.Cleanup(upstream.Close)

	r, _ := newResponsesTestRouter(t, upstream)

	body := `{"model":"openai/gpt-4o","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var respObj map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &respObj); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if respObj["object"] != "response" {
		t.Errorf("object = %v", respObj["object"])
	}
	if respObj["output_text"] != "hi there" {
		t.Errorf("output_text = %v", respObj["output_text"])
	}
	if respObj["status"] != "completed" {
		t.Errorf("status = %v", respObj["status"])
	}
	usage := respObj["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(4) || usage["output_tokens"] != float64(2) {
		t.Errorf("usage = %#v", usage)
	}
}

func TestHandleResponsesStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w,
			"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"+
				"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"+
				"data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)

	r, _ := newResponsesTestRouter(t, upstream)

	body := `{"model":"openai/gpt-4o","input":"hello","stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"response.output_text.delta"`) {
		t.Errorf("stream missing output_text.delta\nbody: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"response.completed"`) {
		t.Errorf("stream missing response.completed\nbody: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: 테스트가 실패하는지 확인**

Run: `go test ./internal/proxy/ -run 'TestHandleResponses' -v`
Expected: FAIL (컴파일 에러 — `HandleResponses` 없음)

- [ ] **Step 3: `HandleResponses` 구현**

`backend/internal/proxy/responses.go`에 추가 (import에 `net/http`, `io`, `strings`, `omnirelay/internal/apiresponse`, `omnirelay/internal/models`, `github.com/gin-gonic/gin` 추가):

```go
func (e *Engine) HandleResponses(c *gin.Context) {
	ensureRequestID(c)
	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	body, ok := readJSONBody(c)
	if !ok {
		return
	}
	if param, err := apiresponse.ValidateResponsesBody(body); err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, err.Error(), param)
		return
	}

	fullModelID := body["model"].(string)

	dbModel, provider, adapter, _, ok := e.resolveDispatch(c, fullModelID, userID, apiresponse.FormatOpenAI)
	if !ok {
		return
	}

	chatBody, err := responsesToChatBody(body)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, err.Error(), "")
		return
	}

	resp, startTime, inputTokens, wroteError := e.buildAndSendChatRequest(c, provider, dbModel, adapter, chatBody, fullModelID, apiKeyID, userID)
	if wroteError {
		return
	}
	defer resp.Body.Close()

	if extractStreamFlag(chatBody) {
		e.handleResponsesStream(c, resp, adapter, apiKeyID, provider.ID, fullModelID, dbModel, userID, provider.ProviderType, inputTokens)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: "failed to read the model response",
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read the model response"})
		return
	}

	chatResp, wrote := parseNonStreamChatResponse(c, respBody, resp.Header, adapter, fullModelID, dbModel, apiKeyID, provider.ID, startTime, userID, e.usageService, provider.ProviderType, inputTokens)
	if wrote || chatResp == nil {
		return
	}
	c.JSON(http.StatusOK, chatResponseToResponses(chatResp, fullModelID))
}
```

**주의:** `parseNonStreamChatResponse` 시그니처는 Task 3에서 `c *gin.Context`를 첫 파라미터로 갖는 것으로 정의했다 — 위 호출부 `parseNonStreamChatResponse(c, respBody, ...)`가 그 시그니처를 따른다.

- [ ] **Step 4: 라우트 등록**

`backend/cmd/server/main.go:117-124`의 v1 그룹에 추가:

```go
	v1 := r.Group("/v1")
	v1.Use(middleware.APIKeyAuth(apiKeyService), bodySizeLimit())
	{
		v1.POST("/chat/completions", proxyEngine.HandleChatCompletions)
		v1.POST("/responses", proxyEngine.HandleResponses)
		v1.GET("/models", proxyEngine.HandleListModels)
		v1.GET("/models/*model", proxyEngine.HandleGetModel)
		v1.POST("/messages", proxyEngine.HandleMessages)
	}
```

- [ ] **Step 5: 전체 테스트 + vet + build**

Run: `go test ./... && go vet ./... && go build ./...`
Expected: 전부 PASS

- [ ] **Step 6: 커밋**

```bash
git add backend/internal/proxy/responses.go backend/internal/proxy/responses_test.go backend/cmd/server/main.go
git commit -m "feat: add /v1/responses endpoint"
```

---

### Task 7: 최종 검증

**Files:**
- 없음 (코드 변경 없음)

- [ ] **Step 1: 전체 검증**

Run: `go test ./... && go vet ./... && go build -o omnirelay ./cmd/server/`
Expected: 전부 PASS

- [ ] **Step 2: 스펙 대비 최종 확인**

`docs/superpowers/specs/2026-08-04-responses-api-design.md`의 체크리스트:
- 요청 변환: Task 1 ✓
- 비스트리밍 응답 변환: Task 2 ✓
- 리팩터링 2건: Task 3·4 ✓
- 스트리밍 변환: Task 5 ✓
- 핸들러 + 라우트: Task 6 ✓
- 범위 컷(YAGNI): `previous_response_id`, `store`, `include`, web_search 툴, `reasoning`, path-routed passthrough — 변경 없음 확인 ✓

## Self-Review 기록

- **스펙 커버리지**: 전 항목 태스크 매핑 완료 (Task 1-7)
- **플레이스홀더**: 없음 — 모든 코드 블록 완전
- **타입 일관성**: `responsesToChatBody(body) (map, error)`, `chatResponseToResponses(chatResp, fullModelID) map`, `parseNonStreamChatResponse(c, respBody, respHeader, adapter, fullModelID, dbModel, apiKeyID, providerID, startTime, userID, usageService, providerType, inputTokens) (map, bool)`, `buildAndSendChatRequest(...) (resp, startTime, inputTokens, wroteError)`, `handleResponsesStream(c, resp, adapter, apiKeyID, providerID, fullModelID, dbModel, userID, providerType, inputTokens)` — Task 간 시그니처 일치
