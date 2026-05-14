# Repository Guidelines

## Project Structure & Module Organization

OmniRelay is a single-Docker-image AI proxy: Go backend (Gin) + Vue 3 dashboard (Vuetify), served behind Caddy as reverse proxy.

- `backend/` — Go module `omnirelay` (Go 1.22), entrypoint `cmd/server/`
  - `internal/handlers/` — thin Gin HTTP handlers
  - `internal/proxy/` — provider adapters (OpenAI, Anthropic, LM Studio, Ollama, Gemini)
  - `internal/service/` — business logic (auth, providers, models, usage)
  - `internal/database/` — SQLite via `modernc.org/sqlite` (pure Go, no CGO); `migrations.go` auto-runs on startup
  - `internal/middleware/` — JWT and API key auth
  - `internal/models/` — shared structs
- `frontend/` — Vue 3 + Vuetify 3 + Pinia + Chart.js, two-space indentation
  - `frontend/Caddyfile` — Caddy v2 config used in Docker image (/etc/caddy/Caddyfile)
- `Dockerfile` — single multi-stage build: frontend (Bun) → backend (Go) → runtime (caddy:2-alpine)
- `OpenAPI-Specification/` — provider reference specs

## Build & Run Commands

```bash
# Backend (from backend/)
go run ./cmd/server/
go build -o omnirelay ./cmd/server/
go test ./...

# Frontend (from frontend/) — use Bun, not npm/yarn/pnpm
bun install
bun run dev          # port 5173, proxies /v1 and /admin to :8080
bun run build        # vue-tsc --noEmit + vite build → dist/
bun run preview
```

`bun run build` runs `vue-tsc --noEmit` for type checking before the Vite build. The README incorrectly references `npm install`; Bun is the actual package manager (see `bun.lock`, Dockerfile).

## Docker

```bash
docker build -t omnirelay .
docker run -p 80:80 omnirelay
```

The Dockerfile builds a **single container** with Caddy + the Go backend. No docker-compose file exists (despite README mention). Backend and Caddy run via shell entrypoint: `/app/omnirelay & caddy run --config /etc/caddy/Caddyfile --adapter caddyfile`.

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

## Testing

No tests are currently checked in. When adding Go tests, place them beside implementations as `*_test.go` and run `go test ./...` from `backend/`. For frontend, `bun run build` provides type safety; manual dashboard testing is the current verification.

## Security

Never commit provider API keys or production secrets. Override `JWT_SECRET` and `ENCRYPT_KEY` via environment variables (dev defaults exist but are unsafe for production). Keep SQLite databases out of commits unless intentionally adding a fixture.

## important

DO NOT USE `rg` command in any shell, terminal.
