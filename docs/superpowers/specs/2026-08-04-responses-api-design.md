# `/v1/responses` 엔드포인트 지원 설계

- 날짜: 2026-08-04
- 상태: 승인됨
- 범위: OmniRelay 백엔드에 OpenAI Responses API(`/v1/responses`) 번역 엔드포인트 추가

## 배경

Vercel AI SDK의 `openai()`(Responses API) 클라이언트를 OmniRelay baseURL로 사용하면
기본적으로 `/v1/responses`를 호출하지만 OmniRelay는 chat completions만 지원해 에러가 발생한다.
OmniRelay는 모든 업스트림(LM Studio/Ollama/Anthropic/Gemini 포함)을 중계하므로,
`/v1/responses` 요청을 기존 chat completions 파이프라인으로 **번역**하는 방식으로 지원한다.

## 아키텍처

Responses 요청 → `responsesToChatBody()` 변환 → 기존 `resolveDispatch` + `buildAndSendChatRequest`(executeChat 분리) 재사용 →
응답을 `chatResponseToResponses()`(비스트리밍) 또는 `handleResponsesStream`(SSE)으로 Responses 형식으로 재변환.

- 인증(`APIKeyAuth`), 모델 해석(`resolveDispatch`), 사용량 로깅, `stream_options: include_usage` 주입은 전부 기존 코드 재사용.
- 에러 형식은 `FormatOpenAI` 그대로 (`/v1/responses` 경로는 `FormatFromPath` 기본값이 OpenAI 스타일).

### 새 파일 / 수정 파일

| 파일 | 내용 |
|---|---|
| `backend/internal/proxy/responses.go` | `HandleResponses` 핸들러 + 순수 함수 `responsesToChatBody`, `chatResponseToResponses` |
| `backend/internal/proxy/responses_stream.go` | `handleResponsesStream` — chat SSE → Responses SSE 번역 |
| `backend/internal/apiresponse/validation.go` | `ValidateResponsesBody` 추가 (model/input 필수) |
| `backend/cmd/server/main.go` | `v1.POST("/responses", proxyEngine.HandleResponses)` 라우트 추가 |
| `backend/internal/proxy/upstream.go` | `handleNonStreamChatResponse`를 `parseNonStreamChatResponse`(chat 응답 map 반환 + 이미 응답을 썼는지 bool)로 분리. 기존 `handleNonStreamChatResponse`는 그 위 얇은 래퍼로 유지 (동작 불변) |
| `backend/internal/proxy/chat_handler.go` | `executeChat`에서 "BuildChatRequest → stream_options 주입 → executeUpstream → 에러 처리" 부분을 `buildAndSendChatRequest(...) (resp, startTime, inputTokens, wroteError)`로 분리. 기존 `executeChat`은 이 함수를 호출해 기존 분기(stream/non-stream) 유지 (동작 불변) |

`HandleResponses` 흐름 (executeChat을 직접 호출할 수 없음 — executeChat이 chat 형식으로 c에 직접 쓰기 때문):

```
1. readJSONBody + ValidateResponsesBody
2. resolveDispatch → dbModel, provider, adapter, apiKey  (기존)
3. chatBody := responsesToChatBody(body)
4. resp, startTime, inputTokens, wroteError := buildAndSendChatRequest(c, provider, dbModel, adapter, chatBody, fullModelID, apiKeyID, userID)
5. stream=true → handleResponsesStream(...)   (Responses SSE 출력)
   stream=false → chatResp, wrote := parseNonStreamChatResponse(...) → chatResponseToResponses(chatResp) → c.JSON
```

## 요청 변환 (Responses → Chat completions)

`responsesToChatBody(body) map[string]interface{}` — 순수 함수:

- `input` string → `messages: [{role: "user", content: <string>}]`
- `input` 배열 → 항목별 매핑:
  - 메시지 `{role, content}`: `content`가 string이면 그대로; 배열이면 part 변환
    (`input_text`→`text`, `input_image`→`image_url` 타입으로 치환)
  - `{type: "function_call_output", call_id, output}` → `{role: "tool", tool_call_id, content}`
  - `{type: "function_call", call_id, name, arguments}` → `{role: "assistant", tool_calls: [{id, type, function:{name, arguments}}]}` (arguments 객체→JSON 문자열)
- `instructions` → system 메시지를 `messages` 맨 앞에 추가
- `max_output_tokens` → `max_tokens`
- `stream`, `temperature`, `top_p`, `stop` → 그대로 전달
- `tools` → `{type:"function", function:{name, description, parameters, strict}}` 형태로 변환.
  function이 아닌 툴(web_search 등)은 드랍
- `model` → 그대로 (기존 `stripProviderPrefix`가 어댑터에서 처리)
- `previous_response_id`, `store`, `include`, `reasoning`, `text`, `metadata` → 무시

## 응답 변환 (Chat completions → Responses, 비스트리밍)

`chatResponseToResponses(chatResp, fullModelID) map[string]interface{}` — 순수 함수.

```json
{
  "id": "resp_<12자리 hex 랜덤>",
  "object": "response",
  "created_at": <unix 초>,
  "status": "completed" | "incomplete",
  "model": "openai/gpt-4o",
  "output": [
    {"id":"msg_<rand>","type":"message","status":"completed","role":"assistant",
     "content":[{"type":"output_text","text":"...","annotations":[]}]},
    {"id":"fc_<rand>","type":"function_call","call_id":"call_<rand>",
     "name":"...","arguments":"{...json 문자열...}"}
  ],
  "output_text": "<전체 텍스트>",
  "usage": {
    "input_tokens": <prompt_tokens>,
    "output_tokens": <completion_tokens>,
    "total_tokens": <합>,
    "input_tokens_details": {"cached_tokens": <cached_tokens, 없으면 0>},
    "output_tokens_details": {"reasoning_tokens": 0}
  }
}
```

- `status`: chat `finish_reason`가 `length` → `incomplete` (그 외 `completed`)
- `output`: chat `message.content`(string) → output_text part; `message.tool_calls` → function_call 아이템
- `output_text`: message part 텍스트만 (function_call 제외)
- usage는 chat 응답의 `usage`에서 추출, 없으면 0

## 스트리밍 변환 (chat SSE → Responses SSE)

`handleResponsesStream` — 기존 `handleStreamResponse` 구조 재사용:

1. `adapter.ParseStreamChunk(chunk, state)`로 토큰/캐시 추출 (기존 방식 그대로)
2. 반환된 transformed(모든 어댑터가 OpenAI chat-format SSE로 수렴)를 파싱해
   `delta.content`, `delta.tool_calls`, `finish_reason` 추출
3. Responses SSE 이벤트로 변환, `streamWriter`로 출력

이벤트 시퀀스:

```
response.created            {"type":"response.created","response":{id:"resp_...","object":"response","status":"in_progress","model":<fullModelID>,"output":[]}}
(첫 텍스트 델타 전)          output_item.added (message 아이템) + content_part.added
response.output_text.delta  * 반복
(종료 시)                    output_text.done → content_part.done → output_item.done
(도구 호출 시)               output_item.added (function_call 아이템)
response.function_call_arguments.delta * 반복 (부분 JSON 문자열)
function_call_arguments.done → output_item.done
response.completed           {"type":"response.completed","response":{...output, usage...}}
```

- output_item 관리: 텍스트 델타와 도구 델타가 뒤섞이면 현재 아이템을 `output_item.done`으로 닫고 새 아이템을 연다
- `finish_reason` = `length` → `response.incomplete` (그 외 `response.completed`)
- 종료 시 사용량 로깅: `handleStreamResponse`와 동일 로직 (input/output/cache 토큰, cost)
- 응답 id는 `resp_`+랜덤 hex (비스트리밍과 동일 생성 함수)

## 검증

`ValidateResponsesBody(body)`:
- `model` string 필수
- `input` string 또는 배열 필수 (빈 배열은 거부)

## 범위 컷 (YAGNI)

- path-routed `/:provider_key/v1/responses`는 기존 passthrough 유지 (OpenAI 업스트림 네이티브 지원)
- `previous_response_id` 멀티턴, `store`, `include`, 비함수 툴(web_search), `reasoning`, `text` 설정 → 미지원
- Anthropic/Gemini 업스트림은 chat 형식으로 수렴된 SSE만 파싱하므로 별도 분기 없음

## 테스트

- `backend/internal/proxy/responses_test.go`:
  - `responsesToChatBody`: input string/배열, function_call_output, function_call, instructions, tools 변환
  - `chatResponseToResponses`: 텍스트, 도구 호출, usage 매핑
  - 스트리밍: `handleResponsesStream`이 생성하는 SSE 이벤트 시퀀스 (업스트림 chat SSE 피드 → 이벤트 검증)
- `go vet ./...` + `go test ./...` 필수
