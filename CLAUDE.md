# OmniRelay

여러 AI 제공자(OpenAI, Anthropic, Gemini, Ollama, LM Studio)를 하나의 OpenAI/Anthropic 호환 API로 묶는 단일 이미지 AI 프록시.

> 상세 가이드는 [AGENTS.md](AGENTS.md) 참고

## Commands

### Backend (`backend/`)

| Command                               | Description      |
| ------------------------------------- | ---------------- |
| `go run ./cmd/server/`                | Start dev server |
| `go build -o omnirelay ./cmd/server/` | Production build |
| `go test ./...`                       | Run all tests    |
| `go vet ./...`                        | Lint check       |

### Frontend (`frontend/`)

| Command         | Description             |
| --------------- | ----------------------- |
| `bun install`   | Install dependencies    |
| `bun run dev`   | Dev server (port 5173)  |
| `bun run build` | Type-check + Vite build |

### Docker

| Command                       | Description         |
| ----------------------------- | ------------------- |
| `docker build -t omnirelay .` | Build single image  |
| `docker compose up -d`        | Run published image |

## Architecture

```
backend/        # Go module (Gin)
  cmd/server/     Entrypoint
  internal/
    config/       Env config
    database/     SQLite init & migrations
    handlers/     Gin HTTP handlers
    hub/          WebSocket hub
    middleware/   JWT & API key auth
    models/       Shared structs
    proxy/        Provider adapters + proxy engine
    service/      Business logic
frontend/       # Vue 3 + Vuetify 3 + Pinia
  src/
    views/        Dashboard pages (7)
    stores/       Pinia stores
    plugins/      Router, Vuetify, i18n
    locales/      ko/en/ja
```

## Key Admin API Routes

| Route                       | Purpose                     |
| --------------------------- | --------------------------- |
| `POST /admin/auth/register` | Register (first = admin)    |
| `POST /admin/auth/login`    | JWT login                   |
| `GET/POST /admin/providers` | Manage providers            |
| `GET/POST /admin/models`    | Manage models               |
| `GET/POST /admin/api-keys`  | Manage API keys (`om-ni-*`) |
| `GET /admin/usage`          | Usage logs                  |
| `GET /admin/stats`          | Dashboard stats             |
| `GET /admin/ws`             | WebSocket realtime updates  |

## Proxy API Routes

| Route                       | Description            |
| --------------------------- | ---------------------- |
| `POST /v1/chat/completions` | Unified chat           |
| `POST /v1/messages`         | Anthropic-compatible   |
| `GET /v1/models`            | List all models        |
| `/:provider/v1/*`           | Path-routed OpenAI API |
| `/:provider/v1beta/*`       | Path-routed beta API   |
| `/:provider/api/*`          | Path-routed native API |

**Auth**: `Authorization: Bearer om-ni-...` (API key)

## Environment

| Var             | Default             | Description            |
| --------------- | ------------------- | ---------------------- |
| `LISTEN_ADDR`   | `:8080`             | Backend listen address |
| `DATABASE_PATH` | `data/omnirelay.db` | SQLite path            |
| `JWT_SECRET`    | dev default         | JWT signing key        |
| `ENCRYPT_KEY`   | dev default         | 32-byte hex AES key    |

Production: override `JWT_SECRET` and `ENCRYPT_KEY`. Generate with `openssl rand -hex 32`.

## Gotchas

- **Caddyfile**: Use `path /path*`, NOT `path_prefix` (not available in caddy:2-alpine)
- **Streaming cache tokens**: Provider field names differ (Anthropic: `cache_creation_input_tokens`, OpenAI: `prompt_tokens_details.cached_tokens`, Gemini: `cached_content_token_count`)
- **OpenAI streaming**: Must inject `stream_options: {"include_usage": true}` for usage data; handled in `proxy.go`
- **SQLite migrations**: Add columns idempotently via `PRAGMA table_info`; no `ALTER TABLE ADD COLUMN IF NOT EXISTS`
- **Build**: Use Bun, not npm/yarn/pnpm; `bun install --frozen-lockfile`
- **Tests**: `test/` Python scripts (main.py, test2.py, test3.py) are integration probes against live proxy, not pytest suites
