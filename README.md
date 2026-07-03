# OmniRelay

OmniRelay는 여러 AI 제공자를 하나의 OpenAI/Anthropic 호환 API로 묶어 주는 단일 이미지 AI 프록시입니다. 대시보드에서 제공자, 모델, API 키, 사용량과 비용을 관리하고, 클라이언트는 하나의 게이트웨이만 바라보면 됩니다.

## 주요 기능

- OpenAI 호환 API: `/v1/chat/completions`, `/v1/models`
- Anthropic Messages 호환 API: `/v1/messages`
- 제공자 경로 라우팅: `/openai/v1/chat/completions`, `/ollama/api/chat` 등
- 지원 제공자: OpenAI, Anthropic, Gemini, Ollama, LM Studio
- 모델 ID 네임스페이스: `provider-key/model-id` 형식 사용
- 스트리밍 지원: SSE 및 일부 네이티브 스트림 프록시
- 모델 자동 동기화: 제공자 API에서 모델 목록 조회
- 모델별 가격 관리: 입력, 출력, 캐시 쓰기, 캐시 읽기 단가
- 사용량/비용 기록: 토큰, 지연 시간, 오류, API 키별 로그 저장
- 웹 대시보드: 인증, 제공자/모델/API 키/사용량 관리
- 단일 Docker 이미지: Vue 정적 파일, Go 백엔드, Caddy reverse proxy 포함

## 아키텍처

```text
Client / SDK
  |
  | Authorization: Bearer om-ni-...
  v
OmniRelay Gateway
  |
  | /v1/chat/completions       OpenAI-compatible chat
  | /v1/messages               Anthropic-compatible messages
  | /v1/models                 Unified model list
  | /:provider/v1/*            Provider-routed OpenAI-compatible API
  | /:provider/v1beta/*        Provider-routed beta/native API
  | /:provider/api/*           Provider-routed native API, e.g. Ollama
  | /admin/*                   Dashboard API, JWT auth
  v
Provider Adapter Layer
  |
  | openai     passthrough / OpenAI format
  | lmstudio   OpenAI-compatible format
  | ollama     OpenAI-compatible and native API routing
  | anthropic  OpenAI <-> Anthropic translation
  | gemini     OpenAI/Anthropic <-> Gemini native translation
  v
Upstream Providers
```

Docker 배포에서는 Caddy가 `:80`에서 프론트엔드를 제공하고 `/admin`, `/v1`, `/:provider/(v1|v1beta|api)` 요청을 컨테이너 내부 Go 서버 `:8080`으로 전달합니다.

## 기술 스택

| 영역            | 기술                         |
| --------------- | ---------------------------- |
| Backend         | Go 1.25, Gin                 |
| Database        | SQLite, `modernc.org/sqlite` |
| Frontend        | Vue 3, Vuetify 3, Pinia      |
| Charts          | Chart.js, vue-chartjs        |
| Runtime         | Caddy 2 Alpine               |
| Package Manager | Bun                          |

## 빠른 시작

### Docker Compose

이미지를 받아 실행합니다.

```bash
cp .env.example .env
docker compose up -d
```

접속 주소:

- Dashboard: `http://localhost`
- Proxy API: `http://localhost/v1/chat/completions`

`compose.yml`은 `ghcr.io/nergis0318/omnirelay:latest` 이미지를 사용하고 SQLite 데이터는 `omnirelay-data` 볼륨의 `/app/data/omnirelay.db`에 저장합니다.

### 로컬 개발

백엔드와 프론트엔드를 별도 프로세스로 실행합니다.

```bash
# Terminal 1: backend
cd backend
go run ./cmd/server/

# Terminal 2: frontend
cd frontend
bun install
bun run dev
```

접속 주소:

- Dashboard: `http://localhost:5173`
- Backend API: `http://localhost:8080`

Vite 개발 서버는 `/admin`과 `/v1` 요청을 `http://localhost:8080`으로 프록시합니다.

## 직접 이미지 빌드

```bash
docker build -t omnirelay .
docker run --rm -p 80:80 --env-file .env -v omnirelay-data:/app/data omnirelay
```

이미지는 다음 순서로 빌드됩니다.

1. `frontend/`를 Bun으로 빌드
2. `backend/`를 정적 Go 바이너리로 빌드
3. `caddy:2-alpine` 런타임 이미지에 프론트엔드 정적 파일, Go 바이너리, Caddyfile 포함

## 환경 변수

| 변수            | 기본값              | 설명                                          |
| --------------- | ------------------- | --------------------------------------------- |
| `LISTEN_ADDR`   | `:8080`             | Go 백엔드 listen 주소                         |
| `DATABASE_PATH` | `data/omnirelay.db` | SQLite DB 경로                                |
| `JWT_SECRET`    | 개발용 기본값       | 대시보드 JWT 서명 키                          |
| `ENCRYPT_KEY`   | 개발용 기본값       | 저장된 제공자 API 키 암호화용 32바이트 hex 키 |

프로덕션에서는 반드시 `JWT_SECRET`과 `ENCRYPT_KEY`를 강한 값으로 설정하세요. `ENCRYPT_KEY`는 64자리 hex 문자열이어야 합니다.

## 최초 설정

1. 대시보드에 접속합니다.
2. 첫 사용자를 등록합니다. 첫 등록 사용자는 관리자입니다.
3. Providers에서 제공자를 추가합니다.
4. 필요하면 모델 자동 동기화를 실행하거나 Models에서 수동으로 모델을 추가합니다.
5. API Keys에서 클라이언트용 키를 발급합니다.
6. 클라이언트 요청의 `Authorization` 헤더에 발급된 `om-ni-...` 키를 사용합니다.

## 제공자 설정 예시

| Provider Type | 일반적인 Base URL                                  | 비고                                  |
| ------------- | -------------------------------------------------- | ------------------------------------- |
| `openai`      | `https://api.openai.com/v1`                        | OpenAI 호환 응답 형식                 |
| `anthropic`   | `https://api.anthropic.com`                        | `x-api-key`, `anthropic-version` 사용 |
| `gemini`      | `https://generativelanguage.googleapis.com/v1beta` | Gemini native API로 변환              |
| `ollama`      | `http://localhost:11434/v1`                        | 모델 동기화는 `/api/tags` 사용        |
| `lmstudio`    | `http://localhost:1234/v1`                         | OpenAI 호환 로컬 서버                 |

`provider_key`는 모델 ID와 경로 라우팅에 사용되는 짧은 식별자입니다. 예를 들어 `provider_key`가 `openai`이고 모델이 `gpt-4o`라면 OmniRelay 모델 ID는 `openai/gpt-4o`입니다.

## API 사용법

### OpenAI 호환 Chat Completions

통합 `/v1` 엔드포인트에서는 모델 ID에 제공자 prefix를 포함합니다.

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

### 제공자 경로 라우팅

경로에 제공자를 포함하면 요청 본문의 `model`에는 upstream 모델명만 넣을 수 있습니다.

```bash
curl http://localhost:8080/openai/v1/chat/completions \
  -H "Authorization: Bearer om-ni-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

다음과 같은 패턴을 지원합니다.

```text
/:provider/v1/*
/:provider/v1beta/*
/:provider/api/*
```

### Anthropic Messages 호환 API

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

경로 라우팅도 사용할 수 있습니다.

```text
POST /anthropic/v1/messages
```

### Gemini

Gemini 제공자는 OpenAI 또는 Anthropic 호환 요청을 Gemini native API로 변환합니다.

```text
POST /gemini/v1/chat/completions
  -> POST /v1beta/models/{model}:generateContent

stream: true
  -> POST /v1beta/models/{model}:streamGenerateContent?alt=sse
```

OmniRelay가 provider API key를 `x-goog-api-key` 헤더로 설정하고, 응답은 클라이언트가 호출한 호환 형식에 맞게 다시 변환합니다.

### Ollama Native API

Ollama는 OpenAI 호환 API와 native API 라우팅을 모두 사용할 수 있습니다.

`OpenAPI-Specification/Ollama.yaml`은 **native** `/api/*` 스키마입니다. `provider_type: ollama`의 `/v1/chat/completions` 어댑터는 upstream이 **OpenAI 호환 `/v1`** 을 제공한다고 가정하며, native 채팅은 `POST /:provider/api/chat` 패스스루를 사용하세요.

```text
POST /ollama/v1/chat/completions   # OpenAI 호환 upstream (/v1) 필요
POST /ollama/api/chat              # Ollama.yaml native 스키마
GET  /ollama/api/tags
```

## 모델과 가격

모델은 자동 동기화 또는 수동 등록으로 관리합니다. 가격은 USD 기준 1M tokens 단위입니다.

| 필드           | 설명                      |
| -------------- | ------------------------- |
| Input Price    | 기본 입력 토큰 단가       |
| Output Price   | 기본 출력 토큰 단가       |
| 5m Cache Write | 5분 캐시 생성 토큰 단가   |
| 1h Cache Write | 1시간 캐시 생성 토큰 단가 |
| Cache Read     | 캐시 읽기 토큰 단가       |

비용 계산식은 다음과 같습니다.

```text
cost = (tokens / 1,000,000) * price
```

Anthropic/Gemini 등에서 반환되는 cache token 사용량은 가능한 경우 사용량 로그에 함께 저장됩니다.

## API 키

- API 키는 `om-ni-` prefix를 사용합니다.
- 원문 키는 생성 시 한 번만 표시됩니다.
- 저장 시 SHA-256 hash만 보관합니다.
- 삭제는 비활성화 방식으로 처리됩니다.
- `rate_limit_rpm`이 0보다 크면 최근 1분 사용량 기준으로 제한합니다.

## 개발 명령어

### Backend

```bash
cd backend
go run ./cmd/server/
go build -o omnirelay ./cmd/server/
go test ./...
```

### Frontend

```bash
cd frontend
bun install
bun run dev
bun run build
bun run preview
```

`bun run build`는 `vue-tsc --noEmit` 타입 체크 후 Vite production build를 실행합니다.

## 저장소 구조

```text
backend/
  cmd/server/              Go server entrypoint
  internal/config/         environment config
  internal/database/       SQLite init and migrations
  internal/handlers/       dashboard/admin handlers
  internal/middleware/     JWT and API key middleware
  internal/models/         shared data models
  internal/proxy/          provider adapters and proxy engine
  internal/service/        auth, providers, models, API keys, usage

frontend/
  src/views/               dashboard pages
  src/stores/              Pinia stores
  src/plugins/             router, Vuetify, i18n
  src/locales/             UI translations
  Caddyfile                production reverse proxy config

OpenAPI-Specification/     provider reference specs
compose.yml                production-style single service compose file
Dockerfile                 single-image build
```

## 데이터베이스

SQLite DB는 시작 시 자동으로 마이그레이션됩니다. 기본 로컬 경로는 `backend` 실행 기준 `data/omnirelay.db`이고, Docker에서는 `/app/data/omnirelay.db`입니다.

주요 테이블:

- `users`
- `providers`
- `models`
- `api_keys`
- `usage_logs`
- `schema_migrations`

## 라이선스

Apache License 2.0
