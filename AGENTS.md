# Repository Guidelines

## Project Structure & Module Organization

OmniRelay is split into a Go API gateway and a Vue dashboard. Backend code lives in `backend/`: `cmd/server/` starts the Gin server, `internal/handlers/` exposes admin routes, `internal/proxy/` contains provider adapters, `internal/service/` owns business logic, and `internal/database/` handles SQLite setup. Runtime data defaults to `backend/data/`. Frontend code lives in `frontend/src/`, with API wiring in `api/`, Pinia stores in `stores/`, router/Vuetify setup in `plugins/`, layouts in `layouts/`, and pages in `views/`. Provider reference specs are stored in `OpenAPI-Specification/`.

## Build, Test, and Development Commands

Run the backend from `backend/`:

```bash
go run ./cmd/server/
go build -o omnirelay ./cmd/server/
go test ./...
```

Run the frontend from `frontend/`:

```bash
npm install
npm run dev
npm run build
npm run preview
```

`npm run dev` starts Vite on port `5173` and proxies `/admin` and `/v1` to the backend on `localhost:8080`. `npm run build` performs TypeScript checking with `vue-tsc` before producing `dist/`.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use tabs from the formatter and keep package names short, lowercase, and aligned with folder names. Keep HTTP handlers thin and route provider-specific translation through `internal/proxy/` adapters.

Frontend files use Vue 3, TypeScript, Pinia, and Vuetify. Follow the existing two-space indentation in `.vue` and `.ts` files. Name views as `PascalCaseView.vue`, stores as lowercase domain files such as `providers.ts`, and composable store exports as `useXStore`.

## Testing Guidelines

No tests are currently checked in. Add Go tests beside implementation files using `*_test.go`, and run `go test ./...` before backend changes are submitted. For frontend logic, add colocated `*.spec.ts` tests if a test runner is introduced; until then, rely on `npm run build` for type safety and manually verify affected dashboard flows.

## Commit & Pull Request Guidelines

This checkout does not include Git metadata, so no existing commit convention can be inferred. Use concise imperative commits, for example `Add Gemini streaming adapter` or `Fix API key revocation`.

Pull requests should describe the backend or frontend area touched, list verification commands, link related issues, and include screenshots or short recordings for dashboard UI changes. Note any configuration or migration impact, especially changes involving `DATABASE_PATH`, `JWT_SECRET`, `ENCRYPT_KEY`, or provider API key handling.

## Security & Configuration Tips

Never commit real provider keys or production secrets. Override development defaults with environment variables, and keep local SQLite databases out of review unless a fixture is intentionally added.
