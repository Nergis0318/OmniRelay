# Repository Guidelines

## Project Structure & Module Organization

OmniRelay is a single-Docker-image AI proxy: Go backend (Gin) + Vue 3 dashboard (Vuetify), served behind Caddy as reverse proxy.

- `backend/` — Go module `omnirelay` (Go 1.25), entrypoint `cmd/server/`
  - `internal/handlers/` — thin Gin HTTP handlers
  - `internal/proxy/` — provider adapters (OpenAI, Anthropic, LM Studio, Ollama, Gemini), shared request/usage helpers in `proxy_helpers.go` and `usage_log.go`
  - `internal/service/` — business logic (auth, providers, models, usage); `provider_service.go` is the single source of truth for upstream model-list fetching (`FetchModelsFromProvider`)
  - `internal/database/` — SQLite via `modernc.org/sqlite` (pure Go, no CGO); `migrations.go` auto-runs on startup
  - `internal/middleware/` — JWT and API key auth
  - `internal/models/` — shared structs
  - `internal/crypto/` — AES encryption helpers for provider API keys
  - `internal/config/` — environment config loading
- `frontend/` — Vue 3 + Vuetify 3 + Pinia + Chart.js, two-space indentation
  - `frontend/Caddyfile` — Caddy v2 config used in Docker image (/etc/caddy/Caddyfile)
- `Dockerfile` — single multi-stage build: frontend (Bun) → backend (Go) → runtime (caddy:2-alpine)
- `compose.yml` — `docker compose up -d` runs the published `ghcr.io/nergis0318/omnirelay:latest` image
- `OpenAPI-Specification/` — provider reference specs

## Build & Verify Commands

```bash
# Backend (from backend/)
go run ./cmd/server/
go build -o omnirelay ./cmd/server/
go test ./...
go vet ./...              # always run before claiming a fix works

# Frontend (from frontend/) — use Bun, not npm/yarn/pnpm
bun install
bun run dev               # port 5173, proxies /v1 and /admin to :8080
bun run build             # vue-tsc --noEmit + vite build → dist/
bun run preview
```

`bun run build` runs `vue-tsc --noEmit` for type checking before the Vite build. The README incorrectly references `npm install`; Bun is the actual package manager (see `bun.lock`, Dockerfile).

## Docker

```bash
docker build -t omnirelay .
docker run -p 80:80 omnirelay

# Or use the published image via Compose:
docker compose up -d
```

The Dockerfile builds a **single container** with Caddy + the Go backend. `compose.yml` at the repo root pulls `ghcr.io/nergis0318/omnirelay:latest` (no local build) and mounts a named volume for the SQLite DB. Backend and Caddy run via shell entrypoint: `/app/omnirelay & caddy run --config /etc/caddy/Caddyfile --adapter caddyfile`.

Environment in container:
- `LISTEN_ADDR=:8080` — backend listen address
- `DATABASE_PATH=/app/data/omnirelay.db` — SQLite DB location

CI (`.github/workflows/docker.yml`) builds multi-arch (amd64/arm64) and pushes to `ghcr.io` on main push and tags.

## Caddyfile Gotchas

The Caddyfile in `frontend/Caddyfile` uses Caddy v2 syntax. **Do NOT use `path_prefix`** — Caddy v2 does not have a built-in `path_prefix` named matcher. Use the `path` matcher with wildcards instead:

```caddy
# Correct
@admin path /admin /admin/*
reverse_proxy @admin localhost:8080

# Wrong — will fail with "module not registered: http.matchers.path_prefix"
@admin path_prefix /admin/
```

The Docker image uses `caddy:2-alpine` which does not include third-party matcher modules.

## Database Migrations

Migrations auto-run on startup via `internal/database/migrations.go`. SQLite schema using `modernc.org/sqlite` (CGO-free).

**Critical rule when modifying tables**: If you add a column to a table that was created in migration v1, you ALSO need a new migration for existing databases. Make the new migration **idempotent** by checking if the column exists first (use `PRAGMA table_info(tablename)`), since SQLite lacks `ALTER TABLE ADD COLUMN IF NOT EXISTS`.

Migration v1 creates: `users`, `providers`, `models`, `api_keys`, `usage_logs`.
Migration v2 creates: `schema_migrations` tracking table.
Migration v3 adds: `users.email`.
Migration v4 adds: `providers.user_id` (idempotent).

## Coding Style

- Go: `gofmt` tabs, short lowercase package names matching folder names
- Frontend: two-space indentation in `.vue` and `.ts`; views as `PascalCaseView.vue`; stores as lowercase files (e.g., `providers.ts`) exported as `useXStore`

## Streaming Token & Cache Extraction

This is the most error-prone area of the codebase. When modifying proxy code, pay attention to:

### OpenAI does NOT include usage in streaming by default

OpenAI API omits `usage` from streaming chunks unless `stream_options: { "include_usage": true }` is in the request body. The proxy must inject this option for all OpenAI-compatible providers (openai, lmstudio, ollama) when `stream: true`. This is handled in:
- `executeChat()` (proxy.go) — for `/v1/chat/completions`
- `executeMessages()` (proxy.go) — for `/v1/messages`
- `handlePathRoutedProxy()` (proxy.go) — for path-routed requests like `/openai/v1/chat/completions`

The helper `isOpenAICompat(providerType string) bool` (proxy.go) gates this injection. Anthropic and Gemini include usage data natively and do NOT need this option.

### Cache token field names differ per provider

| Provider  | Cache Write (input cache creation)     | Cache Read (cache hits)              |
|-----------|----------------------------------------|--------------------------------------|
| Anthropic | `usage.cache_creation_input_tokens`     | `usage.cache_read_input_tokens`      |
| OpenAI    | — (not applicable)                     | `usage.prompt_tokens_details.cached_tokens` |
| Gemini    | — (not applicable)                     | `usageMetadata.cached_content_token_count` |

For **non-streaming**: `extractCacheTokens()` in proxy.go handles the field-name dispatch.

For **streaming**: each adapter's `ParseStreamChunk` / `ParseMessagesStreamChunk` stores cache tokens in `state["cache_write_5m_tokens"]` and `state["cache_read_tokens"]`, which the stream handlers read after the stream ends.

### Streaming has three paths

- `handleStreamResponse` — OpenAI-style SSE (`/chat/completions`), uses `ParseStreamChunk`
- `handleMessagesStreamResponse` — Anthropic-style SSE (`/messages`), uses `ParseMessagesStreamChunk`
- `handleRawStreamResponse` — catch-all passthrough for unknown providers, only logs latency (no token extraction)

Path-routed proxy (`handlePathRoutedProxy`) must use `handleStreamResponse` (not `handleRawStreamResponse`) when a known adapter exists for the provider type.

### Path-routed proxy (`handlePathRoutedProxy`)

This handler processes requests like `/openai/v1/chat/completions` where the provider is in the URL path rather than the model ID. Key gotchas:
- Token extraction and logging happens in the final `else` block (not gated by `dbModel != nil`; cost is only calculated when `dbModel != nil`)
- Must inject `stream_options: { "include_usage": true }` before marshaling when streaming + OpenAI-compatible
- Must dispatch to `handleStreamResponse` or `handleMessagesStreamResponse` when a known adapter exists, only falling back to `handleRawStreamResponse` for truly unknown formats

## Testing

Go test files:
- `internal/proxy/openai_adapter_test.go` — ParseStreamChunk, ParseMessagesStreamChunk, header forwarding
- `internal/proxy/anthropic_adapter_test.go` — Anthropic adapter tests
- `internal/proxy/gemini_adapter_test.go` — Gemini adapter tests
- `internal/proxy/proxy_helpers_test.go` — `applyGeminiStreamingURL`, `buildUpstreamRequest`, `stripProviderPrefix`, `setGenConfig`
- `internal/proxy/http_helpers_test.go` — `extractUsageFromRawResponse`
- `internal/proxy/adapter_response_test.go` — response parsing tests
- `internal/proxy/adapter_request_test.go` — request building tests
- `internal/service/auth_service_test.go` — registration uniqueness
- `internal/service/apikey_service_test.go` — API-key rate limiting
- `internal/crypto/aes_test.go` — encryption

Place new tests beside implementations as `*_test.go` and run `go test ./...` from `backend/`. The frontend has no test runner; `bun run build` runs `vue-tsc --noEmit` for type checking.

The Python scripts under `test/` (`main.py`, `test2.py`, `test3.py`) are runnable integration probes against a live proxy, not pytest cases. They contain hardcoded `om-ni-...` keys that target a specific dev environment.

## Security

Never commit provider API keys or production secrets. Override `JWT_SECRET` and `ENCRYPT_KEY` via environment variables (dev defaults exist but are unsafe for production). `ENCRYPT_KEY` must be a 64-char hex string (32 bytes). Keep SQLite databases out of commits unless intentionally adding a fixture.

## Important

DO NOT USE `rg` command in any shell, terminal.
