# Anthropic interruption 에러 → 표준 503 변환 설계

- 날짜: 2026-08-04
- 상태: 승인됨
- 범위: OmniRelay 백엔드 프록시에서 Anthropic의 interruption 에러("Temporary service interruption. Retry the last turn; your conversation and tool state are preserved.")를 감지해 표준 503 retryable 에러로 변환

## 배경

Anthropic API가 서비스 중단 시 에이전트 클라이언트에 `interruption` 에러를 보낸다. 이 메시지는 다양한 형태로 나타난다:

- 비스트리밍 응답의 `content[0].text` / `choices[0].message.content` (HTTP 200)
- 스트리밍 SSE의 `content_block_delta` text (`HTTP 200`)
- 스트리밍 SSE의 `type: error` 이벤트 (`{"type":"error","error":{"type":"interruption","message":"..."}}`, HTTP 200)

현재 프록시는 이를 정상 모델 출력으로 취급해 그대로 전달한다. 에이전트 도구(Claude Code, OpenAI SDK 등)는 이를 에러로 인식하지 못해 마지막 턴을 재시도하지 않거나 빈/이상 응답으로 오동작한다. 메시지 자체가 "Retry the last turn"을 지시하므로, 재시도 가능한 표준 5xx 에러로 변환해 전달해야 한다.

기존에 동일 패턴이 이미 존재한다: 스트림 text 델타가 `"Request failed."`면 `state["upstream_error"]`로 감지해 502 표준 에러로 변환 (`anthropic_adapter.go:364` → `stream.go:244`). 이번 변경은 이 메커니즘을 재사용하되 임시 장애이므로 503으로 처리한다.

## 감지

`internal/proxy/proxy_helpers.go`에 공유 헬퍼 추가:

```go
// interruptionMarker는 Anthropic이 임시 서비스 중단 시 보내는 문구의 접두사.
const interruptionMarker = "Temporary service interruption"

func isInterruptionText(text string) bool {
    return strings.HasPrefix(strings.TrimSpace(text), interruptionMarker)
}
```

- 접두사 매치: 문구 변형/후속 문장 대비 (기존 "Empty message" 정확 일치 동작은 유지)
- SSE `type: error` 이벤트: `error.type == "interruption"` 또는 `error.message`가 `isInterruptionText` 매치 → 메시지 추출

## 에러 형태

- **Anthropic 형식**: HTTP 503 + `{"type":"error","error":{"type":"overloaded_error","message":"<원문>"},"request_id":...}` — Claude Code 등이 529 overloaded처럼 재시도 처리
- **OpenAI 형식**: HTTP 503 + `{"error":{"message":"<원문>","type":"server_error","code":"upstream_error","request_id":...}}`
- `internal/apiresponse/errors.go`에 `AbortServiceUnavailable(c, format, message)` 헬퍼 추가 (기존 `AbortBadGateway` 스타일; Anthropic → `overloaded_error`, OpenAI → `server_error`)
- 기존 "Request failed."(502 `api_error`)와 "Empty message" 동작은 **변경하지 않음**

## 경로별 변경

| 경로 | 파일:위치 | 변경 |
|---|---|---|
| 비스트리밍 `/v1/messages` | `proxy.go:335` (`executeMessages`) | `extractErrorContent`가 interruption도 반환하도록 확장 → interruption이면 503 |
| 비스트리밍 path-routed | `proxy.go:491` (`handlePathRoutedProxy`) | 동일 |
| 비스트리밍 `/v1/chat/completions` | `upstream.go:112` (`parseNonStreamChatResponse`) | `ParseChatResponse` 후 에러 콘텐츠 체크 **추가** (현재 없음) → 503 |
| 스트리밍 Anthropic 형식 | `anthropic_adapter.go:327` (`ParseMessagesStreamChunk`) + `stream.go:215` (`handleMessagesStreamResponse`) | text_delta 감지 확장 + `case "error"` 이벤트 → `state["upstream_error"]`. 핸들러에서 interruption이면 503, 아니면 기존 502 |
| 스트리밍 OpenAI 형식 | `anthropic_adapter.go:222` (`ParseStreamChunk`) + `stream.go:59` (`handleStreamResponse`) | text_delta 변환 전 감지 + `case "error"` → `state["upstream_error"]`. 핸들러는 **첫 콘텐츠 청크 전까지 출력 버퍼링** (role 청크가 먼저 나가면 503으로 되돌릴 수 없음) → interruption이면 503, 아니면 기존 502 |
| Responses API 스트리밍 | `responses_stream.go:18` (`handleResponsesStream`) | `ParseStreamChunk`가 상태 설정 → 핸들러는 첫 콘텐츠 델타 전까지 이벤트 emit 지연 → interruption이면 503 |

### `extractErrorContent` 확장 (비스트리밍)

현재 `isUpstreamErrorContent`(정확 일치 "Empty message")만 검사. interruption 텍스트도 반환하도록 추가. 호출부(`executeMessages`, `handlePathRoutedProxy`)와 새 호출부(`parseNonStreamChatResponse`)에서 반환 메시지가 interruption이면 503, 아니면 기존 동작(200/`api_error`) 유지.

### 스트리밍 버퍼링 상세

- `handleMessagesStreamResponse`: 이미 전체 버퍼링(`streamBuf`) → 종료 후 상태 확인만으로 충분
- `handleStreamResponse`: `message_start` role 청크, `message_delta` finish 청크는 첫 콘텐츠 전에 나옴. 첫 콘텐츠 텍스트 델타 전까지 변환 출력을 버퍼에 보류. 첫 콘텐츠가 interruption이면 아무것도 쓰지 않고 503. 정상 콘텐츠면 버퍼 flush 후 기존처럼 스트리밍
- `handleResponsesStream`: `response.created`, `output_item.added`, `content_part.added` 등 사전 이벤트를 첫 콘텐츠 델타 전까지 보류. interruption이면 503
- 첫 콘텐츠 이후 interruption이 발견되는 극단적 케이스(문구가 여러 델타로 분할되는 경우)는 감지 못할 수 있음 — Anthropic은 캐넌 에러를 단일 델타로 전송하므로 실질적으로 발생하지 않음

## 사용량 로깅

기존 패턴 유지: 감지 시 `e.logUpstreamError(...)` (스트리밍) / 기존 로그 경로 (비스트리밍). 실패 응답으로 남으므로 토큰/비용 로깅은 하지 않음 (기존 "Request failed."와 동일).

## 테스트

- `proxy_helpers_test.go`: `isInterruptionText` 접두사 매치, `extractErrorContent` interruption 반환
- `anthropic_adapter_test.go`: `ParseMessagesStreamChunk`/`ParseStreamChunk` — interruption text 델타 + `type: error` 이벤트 → `state["upstream_error"]` 설정, 정상 델타는 미변경
- 스트림 핸들러 테스트: interruption 스트림 → 503 + Anthropic `overloaded_error` / OpenAI `server_error` 형태 검증; 정상 스트림은 그대로 통과
- `go test ./...`, `go vet ./...` 통과

## 비범위 (제외)

- "Request failed."/"Empty message" 동작 변경
- OpenAI/기타 프로바이더의 유사 문구 감지 (Anthropic 전용)
- `Retry-After` 헤더
