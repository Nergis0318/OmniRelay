# OmniRelay

OmniRelay는 여러 AI 제공자(OpenAI, Anthropic, Gemini, Ollama, LM Studio)를 하나의 OpenAI/Anthropic 호환 API로 통합하는 단일 이미지 AI 프록시입니다. 대시보드에서 제공자, 모델, API 키, 사용량과 비용을 관리하고, 클라이언트는 하나의 게이트웨이만 바라보면 됩니다.

---

## 주요 기능

- **OpenAI / Anthropic 호환 API** — `/v1/chat/completions`, `/v1/messages`, `/v1/models`
- **제공자 경로 라우팅** — `/:provider/v1/*`, `/:provider/v1beta/*`, `/:provider/api/*`
- **URL 패스스루 릴레이** — `/https://api.openai.com/v1/...` 형태는 무변환 중계 + 성능(DNS/연결/TTFB/TTFT/전송량)만 별도 측정
- **5개 제공자 지원** — OpenAI, Anthropic, Gemini, Ollama, LM Studio
- **스트리밍** — SSE 스트리밍 및 토큰/비용 추출
- **모델 자동 동기화** — 제공자 API에서 모델 목록 자동 조회 및 등록
- **소스 모델 가져오기** — 등록된 다른 제공자의 모델을 선택해서 가져오기
- **캐시 토큰 비용** — Anthropic/Gemini/OpenAI 캐시 읽기/쓰기 토큰 추출 및 비용 계산
- **사용량/비용 기록** — 토큰, 지연 시간, 오류, API 키별 로그 저장
- **API 키 인증** — `om-ni-` prefix 키, SHA-256 해시 보관, RPM 제한, 토큰 한도
- **실시간 대시보드** — WebSocket으로 사용량/로그를 실시간 업데이트 (재연결 지원)
- **3개 국어 UI** — 한국어, English, 日本語
- **반응형 디자인** — 데스크톱 사이드바 + 모바일 하단 탭 바
- **다크 테마** — Amber/Teal 계열의 커스텀 Vuetify 테마
- **단일 Docker 이미지** — Vue 정적 파일, Go 백엔드, Caddy reverse proxy 포함 (멀티 아키텍처: amd64 + arm64)

---

## 아키텍처

```text
Client / SDK
  |
  | Authorization: Bearer om-ni-...
  v
OmniRelay Gateway                         ← Caddy reverse proxy (:80)
  |                                           ── 정적 파일: Vue SPA
  | /v1/chat/completions                   │   ── 프록시: Go 백엔드 (:8080)
  | /v1/messages                           │
  | /v1/models                             │
  | /:provider/v1/*  /v1beta/*  /api/*     │
  | /admin/*          (JWT 인증)           │
  | /health  /readyz                       │
  v
Provider Adapter Layer                    ← 요청/응답 변환
  |
  | openai/openai     Passthrough — OpenAI 형식
  | lmstudio/openai   Passthrough — OpenAI 호환 형식
  | ollama/openai     Passthrough — OpenAI 호환 형식
  | anthropic         변환 — OpenAI ↔ Anthropic Messages
  | gemini            변환 — OpenAI/Anthropic ↔ Gemini Native API
  v
Upstream Providers                        ← 각 제공자 원본 API
```

---

## 기술 스택

| 영역          | 기술                                           |
| ------------- | ---------------------------------------------- |
| Backend       | Go 1.25, Gin                                   |
| Database      | SQLite (`modernc.org/sqlite`, CGO 불필요)      |
| Frontend      | Vue 3, Vuetify 3, Pinia, vue-router, vue-i18n  |
| Charts        | Chart.js, vue-chartjs                          |
| Realtime      | gorilla/websocket                              |
| Runtime       | Caddy 2 Alpine (single container)              |
| 패키지 관리자 | Bun                                            |
| CI/CD         | GitHub Actions + Blacksmith (multi-arch build) |

---

## 빠른 시작

### Docker Compose (권장)

```bash
cp .env.example .env
# .env에서 JWT_SECRET, ENCRYPT_KEY를 강력한 값으로 설정하세요
docker compose up -d
```

접속 주소: **http://localhost**

- Dashboard: `http://localhost`
- Proxy API: `http://localhost/v1/chat/completions`

> `compose.yml`은 `ghcr.io/nergis0318/omnirelay:latest` 이미지를 사용합니다. SQLite 데이터는 `omnirelay-data` 볼륨에 저장됩니다.

### 로컬 개발

```bash
# Terminal 1: backend
cd backend
go run ./cmd/server/

# Terminal 2: frontend
cd frontend
bun install
bun run dev
```

- Dashboard: `http://localhost:5173`
- Backend API: `http://localhost:8080`

Vite 개발 서버는 `/v1`, `/admin` 요청을 `:8080`으로 프록시합니다.

### 직접 이미지 빌드

```bash
docker build -t omnirelay .
docker run --rm -p 80:80 --env-file .env -v omnirelay-data:/app/data omnirelay
```

빌드 순서: `frontend/` (Bun) → `backend/` (정적 Go 바이너리) → `caddy:2-alpine` 런타임에 포함

---

## 환경 변수

| 변수            | 기본값                                        | 설명                                   |
| --------------- | --------------------------------------------- | -------------------------------------- |
| `LISTEN_ADDR`   | `:8080`                                       | Go 백엔드 listen 주소                  |
| `DATABASE_PATH` | `data/omnirelay.db`                           | SQLite DB 경로                         |
| `JWT_SECRET`    | (개발용 기본값)                               | 대시보드 JWT 서명 키                   |
| `ENCRYPT_KEY`   | (개발용 기본값)                               | 제공자 API 키 암호화용 32바이트 hex 키 |
| `CORS_ORIGINS`  | `http://localhost:5173,http://localhost:3000` | 허용할 CORS 출처 (쉼표 구분)           |
| `PASSTHROUGH_ENABLED` | `true`                                | URL 패스스루 릴레이(`/<upstream-url>`) 열기/닫기 |
| `PASSTHROUGH_ALLOW_PRIVATE` | `false`                     | `true`면 사설/루프백 주소로 릴레이 허용 (로컬 Ollama·LM Studio 측정용) |
| `PASSTHROUGH_TIMEOUT_SECONDS` | `900`                       | 릴레이 1건 상한(초). 스트리밍 본문 읽기까지 포함 |

> **프로덕션**에서는 반드시 `JWT_SECRET`과 `ENCRYPT_KEY`를 강력한 값으로 설정하세요. `ENCRYPT_KEY`는 64자리 hex 문자열이어야 합니다 (`openssl rand -hex 32`).

---

## 최초 설정

1. 대시보드(`http://localhost`)에 접속합니다.
2. 첫 사용자를 등록합니다. **첫 등록 사용자는 관리자**입니다. (이메일 기반 인증)
3. **Providers**에서 제공자를 추가하고 API 키를 설정합니다.
4. **모델 자동 동기화**를 실행하거나, 다른 제공자의 모델을 **소스 모델**에서 선택해 가져옵니다.
5. **API Keys**에서 클라이언트용 키를 발급합니다.
6. 클라이언트 요청의 `Authorization` 헤더에 `om-ni-...` 키를 사용합니다.

---

## 제공자 설정

| Provider Type | 일반적인 Base URL                                  | 비고                                       |
| ------------- | -------------------------------------------------- | ------------------------------------------ |
| `openai`      | `https://api.openai.com/v1`                        | OpenAI 호환 응답 형식                      |
| `anthropic`   | `https://api.anthropic.com`                        | `x-api-key`, `anthropic-version` 자동 설정 |
| `gemini`      | `https://generativelanguage.googleapis.com/v1beta` | Gemini Native API로 변환                   |
| `ollama`      | `http://localhost:11434/v1`                        | 모델 동기화는 `/api/tags` 사용             |
| `lmstudio`    | `http://localhost:1234/v1`                         | OpenAI 호환 로컬 서버                      |

> **provider_key**는 모델 ID와 경로 라우팅에 사용되는 짧은 식별자입니다. 예: `provider_key: openai`, 모델 `gpt-4o` → OmniRelay 모델 ID: `openai/gpt-4o`

---

## API 사용법

### Chat Completions (OpenAI 호환)

통합 `/v1` 엔드포인트 — 모델 ID에 제공자 prefix 포함:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer om-ni-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### Messages (Anthropic 호환)

```bash
curl http://localhost:8080/v1/messages \
  -H "Authorization: Bearer om-ni-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "anthropic/claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### 제공자 경로 라우팅

경로에 제공자를 포함하면 요청 본문에 upstream 모델명만 넣을 수 있습니다.

```bash
curl http://localhost:8080/openai/v1/chat/completions \
  -H "Authorization: Bearer om-ni-..." \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

지원 패턴:

| 패턴                  | 예시                          | 용도                   |
| --------------------- | ----------------------------- | ---------------------- |
| `/:provider/v1/*`     | `/openai/v1/chat/completions` | OpenAI 호환 API        |
| `/:provider/v1beta/*` | `/gemini/v1beta/models`       | Gemini beta API        |
| `/:provider/api/*`    | `/ollama/api/chat`            | Native API (Ollama 등) |

> Caddy가 `:80`에서 위 패턴들을 내부 Go 서버 `:8080`으로 프록시합니다.

### URL 패스스루 (성능 전용 측정)

경로에 **업스트림 URL 전체**를 넣으면 어떤 변환도 없이 그대로 중계합니다. 제공자 등록, 모델 해석, 포맷 변환, 토큰·비용 집계는 전혀 작동하지 않고 **성능만** 별도 테이블에 기록됩니다.

```bash
curl https://omni.xeon.kr/https://api.openai.com/v1/chat/completions \
  -H "Authorization: Bearer sk-업스트림-자체-키" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

- 메서드·헤더·쿼리스트링·본문 바이트가 원본 그대로 전달됩니다. `Authorization`과 `x-api-key`도 그대로 통과시키므로 **호출자가 업스트림 키를 직접** 보유해야 합니다 (리레이 키로 주입하지 않음).
- 응답은 버퍼링 없이 청크 단위로 플러시하므로 SSE가 업스트림 발생 시점 그대로 도착합니다.
- `http:`/`https:`만 허용하고, 사설·루프백·링크로컬(169.254.169.254 포함)·CGNAT 주소는 연결 직전에 해석된 IP로 차단합니다(SSRF 가드). 로컬 모델 서버를 측정할 때는 `PASSTHROUGH_ALLOW_PRIVATE=true`로 켭니다.

응답 헤더로 측정치가 함께 반환됩니다.

| 헤더                | 의미                                  |
| ------------------- | ------------------------------------- |
| `X-Omni-Relay`      | `passthrough` (패스스루 경로임을 표시) |
| `X-Omni-Target`     | 실제 접속한 업스트림 호스트            |
| `X-Omni-Dns-Ms`     | DNS 해석 시간 (연결 재사용이면 없음)   |
| `X-Omni-Connect-Ms` | TCP 연결 시간                         |
| `X-Omni-Tls-Ms`     | TLS 악수 시간                         |
| `X-Omni-Ttfb-Ms`    | 업스트림 응답 헤더까지의 시간          |

DB 기록은 `usage_logs`와 분리된 `passthrough_logs` 테이블로, 별도 goroutine에서 비동기로 씁니다(측정 경로에 지연을 주지 않음). 조회는 관리자 JWT로 합니다.

```bash
# 집계: p50/p95/p99, TTFB/TTFT, DNS/연결/TLS 평균, 에러율, 초당 요청
curl http://localhost:8080/admin/passthrough/performance -H "Authorization: Bearer <JWT>"
curl "http://localhost:8080/admin/passthrough/performance?host=api.openai.com&granularity=minute" -H "Authorization: Bearer <JWT>"

# 개별 측정 기록
curl "http://localhost:8080/admin/passthrough/logs?limit=50" -H "Authorization: Bearer <JWT>"
```

**대시보드 → 패스스루**(`/passthrough`, 관리자만 메뉴에 표시)에서도 같은 데이터를 볼 수 있습니다. 지연 단계별 평균과 P50/P95/P99를 하나의 척도로 비교하는 사다리, 호스트별 비교(행 클릭 시 필터), 최근 측정 테이블, 5초 간격 LIVE 갱신을 제공합니다.

### Gemini

Gemini 제공자는 OpenAI 또는 Anthropic 호환 요청을 Gemini Native API로 변환합니다.

```text
POST /gemini/v1/chat/completions → POST /v1beta/models/{model}:generateContent
stream: true                     → POST /v1beta/models/{model}:streamGenerateContent?alt=sse
```

> API 키는 `x-goog-api-key` 헤더로 전송됩니다.

### Ollama Native API

Ollama는 OpenAI 호환 라우팅(`/v1/*`)과 native API 패스스루(`/api/*`)를 모두 지원합니다.

```text
POST /ollama/v1/chat/completions   # OpenAI 호환 upstream (/v1) 필요
POST /ollama/api/chat              # Ollama native 스키마
GET  /ollama/api/tags
```

### 모델 목록 조회

```bash
curl http://localhost:8080/v1/models -H "Authorization: Bearer om-ni-..."
```

> `show_in_model_list`가 활성화된 제공자의 모델만 표시됩니다.

### Health Check

```bash
curl http://localhost:8080/health     # {"status":"ok"}
curl http://localhost:8080/readyz     # {"status":"ready"} (DB 연결 확인)
```

---

## 모델과 가격

모델은 자동 동기화 또는 수동 등록으로 관리합니다. 가격은 USD 기준 **1M tokens** 단위입니다.

| 필드           | 설명                                |
| -------------- | ----------------------------------- |
| Input Price    | 기본 입력 토큰 단가                 |
| Output Price   | 기본 출력 토큰 단가                 |
| 5m Cache Write | 5분 캐시 생성 토큰 단가 (Anthropic) |
| 1h Cache Write | 1시간 캐시 생성 토큰 단가           |
| Cache Read     | 캐시 읽기 토큰 단가                 |

```text
cost = (tokens / 1,000,000) * price
```

> Anthropic(`cache_creation_input_tokens`, `cache_read_input_tokens`), Gemini(`cached_content_token_count`), OpenAI(`cached_tokens`)의 캐시 토큰 사용량이 자동 추출되어 사용량 로그에 저장됩니다.

---

## API 키

- **Prefix**: `om-ni-`
- **저장**: 생성 시 원문 키는 한 번만 표시, DB에는 SHA-256 해시만 저장
- **삭제**: 비활성화(soft delete) 방식
- **RPM 제한**: `rate_limit_rpm` > 0이면 분당 요청 수 제한
- **토큰 한도**: `total_token_limit` > 0이면 총 토큰 사용량 제한

---

## 대시보드

| 페이지    | 설명                                             |
| --------- | ------------------------------------------------ |
| Dashboard | 오늘 비용/요청/토큰, 30일 추이 차트, 시스템 상태 |
| Providers | 제공자 CRUD, API 키 관리, 모델 동기화            |
| Models    | 모델 등록/수정, 가격 설정, 소스 모델 가져오기    |
| API Keys  | 키 발급/취소, RPM 및 토큰 한도 설정              |
| Usage     | 통합 사용량 통계 및 30일 차트 (캐시 토큰 포함)   |
| Logs      | 요청별 상세 내역 (토큰, 지연 시간, 비용, 오류)   |

> Dashboard는 WebSocket을 통해 사용량과 로그를 **실시간**으로 업데이트합니다. 재연결 및 백오프를 지원합니다.

---

## 프로젝트 구조

```text
backend/
  cmd/server/              Go 서버 진입점
  internal/config/         환경 설정 로드
  internal/database/       SQLite 초기화 및 마이그레이션 (v9)
  internal/handlers/       대시보드 API 핸들러
  internal/middleware/     JWT 및 API 키 인증 미들웨어
  internal/models/         공유 데이터 모델
  internal/proxy/          제공자 어댑터 및 프록시 엔진
  internal/service/        인증, 제공자, 모델, API 키, 사용량
  internal/hub/            WebSocket 실시간 허브
  internal/crypto/         AES 제공자 API 키 암호화
  internal/apiresponse/    오류/검증 공유 유틸리티

frontend/
  src/views/               대시보드 페이지 (6개)
  src/stores/              Pinia 스토어
  src/plugins/             라우터, Vuetify, i18n
  src/locales/             UI 번역 (EN/JA/KO)
  src/api/                 HTTP 클라이언트, WebSocket
  src/layouts/             반응형 레이아웃 (데스크톱/모바일)
  Caddyfile                프로덕션 Caddy 설정

OpenAPI-Specification/     제공자 API 스펙 참조
compose.yml                Docker Compose 설정
Dockerfile                 단일 이미지 빌드
.github/workflows/         CI/CD (multi-arch build & push)
```

---

## 개발 명령어

### Backend

```bash
cd backend
go run ./cmd/server/         # 개발 서버 실행
go build -o omnirelay ./cmd/server/  # 빌드
go test ./...                # 테스트
go vet ./...                 # 정적 분석
```

### Frontend

```bash
cd frontend
bun install                  # 의존성 설치
bun run dev                  # 개발 서버 (:5173)
bun run build                # 타입 체크 + 프로덕션 빌드
bun run preview              # 빌드 결과물 미리보기
```

> `bun run build`는 `vue-tsc --noEmit` 타입 체크 후 Vite 빌드를 실행합니다.

---

## 데이터베이스

SQLite (`modernc.org/sqlite`, CGO 불필요). 시작 시 자동 마이그레이션 (v14).

| 테이블               | 설명                                          |
| -------------------- | --------------------------------------------- |
| `users`              | 사용자 계정 (이메일 기반 인증)                |
| `providers`          | 업스트림 제공자 연결 정보                     |
| `models`             | 등록된 모델 및 가격                           |
| `api_keys`           | API 키 (SHA-256 해시, RPM/토큰 제한)          |
| `usage_logs`         | 요청별 사용량 로그 (캐시 토큰 포함)           |
| `passthrough_logs`   | URL 패스스루 릴레이의 성능 측정 (토큰/비용 없음) |
| `schema_migrations`  | 마이그레이션 버전 관리                        |

기본 경로: 로컬 `data/omnirelay.db`, Docker `/app/data/omnirelay.db`

---

## 라이선스

Apache License 2.0
