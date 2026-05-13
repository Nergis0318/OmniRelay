# OmniRelay

Unified AI Inference Gateway — OpenAI/Anthropic-compatible API with multi-provider routing, usage tracking, and dashboard management.

## Features

- **OpenAI-compatible API** — `/v1/chat/completions`, `/v1/models`, streaming via SSE
- **Anthropic-compatible API** — `/v1/messages` with bidirectional format translation
- **Path-based routing** — route by provider key in URL: `POST /openai/v1/chat/completions`
- **Multi-provider** — OpenAI, Anthropic, LM Studio, Ollama, Gemini
- **Gemini Native API** — full format translation between OpenAI and Gemini native `:generateContent` / `:streamGenerateContent`
- **Unified model list** — auto-fetch from providers or manually add models
- **Model ID format** — `provider-key/model-id` (e.g., `openai/gpt-4o`, `gemini/gemini-2.5-flash`)
- **Tiered pricing** — per-model pricing: Input, Output, 5m Cache Write, 1h Cache Write, Cache Read (all $/1M tokens)
- **Cache token tracking** — `cache_creation_input_tokens` / `cache_read_input_tokens` preserved in usage
- **Dashboard** — token usage, cost, latency charts, provider management, API key issuance

## Architecture

```
Client (SDK/App)
    │  OpenAI-format request: model="openai/gpt-4o"
    ▼
┌──────────────────────────┐
│     OmniRelay Gateway    │
│     :8080                │
│                          │
│  /v1/chat/completions    │◄── OpenAI-compatible
│  /v1/models              │◄── Unified model list
│  /v1/messages            │◄── Anthropic-compatible
│  /:provider/v1/*endpoint │◄── Path-based routing
│  /:provider/api/*endpoint│◄── Ollama native routing
│                          │
│  /admin/*                │◄── Dashboard API (JWT)
└──────┬───────────────────┘
       │  Resolve provider, translate format
       ▼
┌──────────────────────────┐
│   Provider Adapter Layer │
│                          │
│  OpenAI    → Passthrough │
│  Anthropic → Translate   │
│  LM Studio → Passthrough │
│  Ollama    → /api/tags   │
│  Gemini    → Translate   │
└──────────────────────────┘
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.22 + Gin |
| Database | SQLite (pure Go) |
| Frontend | Vue 3 + Vuetify 3 |
| Charts | Chart.js + vue-chartjs |

## Quick Start

```bash
# Terminal 1 — Backend
cd backend
go run ./cmd/server/

# Terminal 2 — Frontend
cd frontend
bun install
bun run dev
```

Dashboard: `http://localhost:5173` — first registered user becomes admin.  
Proxy: `http://localhost:8080/v1/chat/completions`

## Docker

Build and run the full stack with Compose:

```bash
docker compose up --build
```

Dashboard: `http://localhost:5173`  
Proxy: `http://localhost:8080/v1/chat/completions`

The backend SQLite database is stored in the `omnirelay-data` named volume. For production deployments, set strong values for `JWT_SECRET` and `ENCRYPT_KEY` before starting Compose.

Build images separately:

```bash
docker build -t omnirelay-backend ./backend
docker build -t omnirelay-frontend ./frontend
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | Backend listen address |
| `DATABASE_PATH` | `data/omnirelay.db` | SQLite database path |
| `JWT_SECRET` | (dev default) | JWT signing secret for dashboard auth |
| `ENCRYPT_KEY` | (dev default) | 64 hex chars (32 bytes) AES-256 key for provider API keys |

## API Usage

### 1. Model ID with provider prefix

```bash
POST /v1/chat/completions
Authorization: Bearer om-ni-xxxx

{
  "model": "openai/gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": true
}
```

### 2. Provider in URL path

```bash
POST /openai/v1/chat/completions
Authorization: Bearer om-ni-xxxx

{
  "model": "gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}]
}
```

Works with any provider key: `/gemini/v1/chat/completions`, `/ollama/v1/chat/completions`, etc.

### 3. Gemini Native

Gemini requests are automatically translated to Google's native format:

```
POST /gemini/v1/chat/completions  →  POST generativelanguage.googleapis.com/v1beta/models/{model}:generateContent
                                     (streaming → :streamGenerateContent?alt=sse)

Auth:  x-goog-api-key instead of Bearer
Body:  OpenAI messages → Gemini contents + system_instruction
Resp:  Gemini candidates → OpenAI choices
```

### 4. Anthropic-compatible

```bash
POST /v1/messages
Authorization: Bearer om-ni-xxxx

{
  "model": "anthropic/claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [{"role": "user", "content": "Hello"}]
}

# Or path-based
POST /anthropic/v1/messages
```

### 5. Ollama native routing

Ollama supports both OpenAI-compat (`/v1/*`) and native (`/api/*`) paths:

```bash
POST /ollama/api/chat
POST /ollama/v1/chat/completions
GET  /ollama/api/tags
```

Model sync uses `/api/tags` (Ollama native spec).

## Provider Setup

| Provider | Base URL | Default Port | Type |
|----------|----------|-------------|------|
| OpenAI | `https://api.openai.com/v1` | — | `openai` |
| Anthropic | `https://api.anthropic.com` | — | `anthropic` |
| LM Studio | `http://localhost:1234/v1` | 1234 | `lmstudio` |
| Ollama | `http://localhost:11434/v1` | 11434 | `ollama` |
| Gemini | `https://generativelanguage.googleapis.com/v1beta` | — | `gemini` |

All providers support the **Auto-fetch** checkbox when adding — it pulls available models from the provider's API.

## Model Pricing

Per-model pricing in dollars per 1M tokens:

| Field | Description |
|-------|-------------|
| Input Price | Base input tokens |
| Output Price | Base output tokens |
| 5m Cache Write | 5-minute cache creation |
| 1h Cache Write | 1-hour cache creation |
| Cache Read | Cache hits and refreshes |

Cost = `(tokens / 1,000,000) × price`. Cache tokens are parsed from `cache_creation_input_tokens` / `cache_read_input_tokens` in the API response and preserved in the OpenAI-format usage object.

## Streaming

All providers support streaming via SSE (`"stream": true`). Token usage is extracted from stream events:

- **Anthropic** — from `message_start` (input) and `message_delta` (output) events
- **Gemini** — from `usageMetadata` in the final stream chunk
- **OpenAI / LM Studio / Ollama** — passed through as-is

## API Key Management

- Keys use `om-ni-` prefix (e.g., `om-ni-5806...`)
- Issue from Dashboard → API Keys
- Revocation via deactivation (soft-delete)
- Optional rate limiting (RPM)

## Development

```bash
# Backend
cd backend
go run ./cmd/server/          # Run
go build -o omnirelay ./cmd/server/  # Build

# Frontend
cd frontend
bun run dev                   # Dev server (port 5173)
bun run build                 # Production build
npx vue-tsc --noEmit          # Type check
```

## License

MIT
