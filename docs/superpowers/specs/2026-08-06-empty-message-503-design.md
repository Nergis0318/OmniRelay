# "Empty message" → 표준 503 변환 설계

- 날짜: 2026-08-06
- 상태: 승인됨
- 범위: OmniRelay 백엔드 프록시에서 업스트림이 "[Empty message]"를 콘텐츠로 전달하는 것을 감지해 표준 503 retryable 에러로 변환 (모든 스트림 경로)

## 배경

conduit류 릴레이 업스트림은 모델 응답이 실패/중단되면 정상 완료 응답(HTTP 200)에 `[Empty message]` 텍스트를 콘텐츠로 실어 보낸다. 에이전트 클라이언트(OpenCode 등)는 이를 에러로 인식하지 못해 `[Empty message]`를 최종 답변으로 표시하고 멈춘다.

mitm 캡처(`error-log.mitm`) 분석 결과 실제 흐름:

- OpenCode → OmniRelay `POST /conduit/v1/responses` (path-routed) → conduit 릴레이
- conduit이 **Responses-API 형식 SSE**(`event: response.output_text.delta`)로 `[Empty message]`를 스트리밍 (reasoning 이벤트 포함)
- 텍스트가 `"[Empty"` + `" message]"` **두 청크로 분할 도착**
- OmniRelay의 기존 감지(어댑터의 `choices[].delta.content` 단일 청크 정확 일치)로는 감지 불가 → 그대로 통과, 클라이언트가 `response.completed` + `[Empty message]` 수신

기존 상태: 채팅/메시지 형식 스트림에서 "Empty message"는 어댑터가 청크 단위로 감지해 레거시 **200 `api_error`** 로 변환 중 (`commit 23ab1c6`). Responses 형식 스트림(conduit)은 완전히 미감지.

## 결정 사항

- `[Empty message]` → **HTTP 503 + retryable 에러** (interruption과 동일 처리) — 기존 200 `api_error` 레거시 동작 대체
- 적용 범위: 모든 스트림 경로 + 비스트리밍 일관화
- Responses 형식 스트림은 감지를 위해 **완료 시점까지 버퍼링** 후 판단 (라이브 스트리밍 불가 트레이드오프 수용 — conduit은 텔레그램 릴레이라 어차피 버퍼링 구조)

## 에러 형태

interruption과 동일한 503 분기 재사용 (`writeStreamUpstreamError`):

- **Anthropic 형식**: HTTP 503 + `{"type":"error","error":{"type":"overloaded_error","message":"<원문>"},...}`
- **OpenAI 형식**: HTTP 503 + `{"error":{"type":"server_error","message":"<원문>","code":"upstream_error",...}}`

## 변경 사항

| 경로 | 파일 | 변경 |
|---|---|---|
| 스트림 에러 변환 | `stream.go` (`writeStreamUpstreamError`) | "Empty message" 분기(200 api_error) 제거 → interruption 분기와 병합해 503. 채팅/메시지/Responses 스트림은 이미 어댑터가 감지하므로 자동 적용 |
| 비스트리밍 | `proxy_helpers.go` (`abortErrorContent`) | "Empty message" → interruption처럼 503 (스트림/비스트리밍 일관화) |
| Responses 형식 스트림 (신규 감지) | `stream.go` (`handleStreamResponse`, `handleRawStreamResponse`) | 청크에 `"type":"response."` 이벤트(Responses 형식) 감지 시 버퍼링 모드: 전체 스트림을 버퍼에 누적 + `response.output_text.delta` 텍스트 누적. 종료 시 누적 텍스트가 "Empty message"면 503, 아니면 버퍼 재생 |
| Responses 형식 비스트리밍 (자동) | `proxy.go` (`handlePathRoutedProxy`) | `extractErrorContent`가 Responses 형식 `response.output`의 텍스트도 검사하도록 확장 → `abortErrorContent` → 503. 검사 대상: 기존 `choices[0].message.content` + 신규 `output[].content[].text` |

### Responses 형식 스트림 감지 상세 (`handleStreamResponse` / `handleRawStreamResponse`)

- 트리거: 청크 바이트에 `"type":"response.` 서브스트링 존재 (response.created / output_text.delta / completed 등 모든 Responses 이벤트 공통)
- Responses 형식으로 판정되면 이후 청크를 전부 버퍼에 보류하고 텍스트 누적: `response.output_text.delta`의 `delta` 필드 + `response.output_text.done`의 `text` 필드 + `response.completed`의 `output_text` 필드 (델타 없이 done/completed만 오는 업스트림 대비)
- 종료 시점(stream end 또는 `data: [DONE]`): 누적 텍스트(trim)가 "Empty message" 정확 일치 → `e.logUpstreamError` + `writeStreamUpstreamError` → 503. 아니면 버퍼를 순서대로 재생 후 기존 로직(사용량 로깅 등) 계속
- 채팅 형식(비 Responses)은 기존 동작 그대로 (버퍼링 없음)
- keepalive는 기존대로 동작 (버퍼링 중에도 플러시 방지됨 — `sw` 대신 버퍼에 쓰므로)

## 사용량 로깅

기존 패턴 유지: 감지 시 `e.logUpstreamError(...)`. 실패 응답이므로 토큰/비용 로깅 없음 (기존 "Request failed."와 동일).

## 테스트

- 기존 `TestNonStreamChatEmptyMessageStill200`, `TestHandleStreamResponseEmptyMessageStill200`, `TestHandleMessagesStreamResponseEmptyMessageStill200` → 503 기대값 + `server_error`/`overloaded_error` 타입으로 수정
- 신규: Responses 형식 스트림 테스트 — `response.output_text.delta` 분할(`"[Empty"` + `" message]"`) → 503; 정상 Responses 스트림 → 버퍼 재생(내용 보존) 검증
- 신규: `extractErrorContent` Responses 형식 검사 테스트
- `go test ./...`, `go vet ./...` 통과

## 비범위 (제외)

- "Request failed."(502) 동작 변경
- `Retry-After` 헤더
- 프론트엔드 변경
- 503 변환 이외의 Responses 형식 스트림 처리 개선 (라우팅, 파싱 확장 등)
