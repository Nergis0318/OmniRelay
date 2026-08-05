# Multi-Format Provider Endpoints Design

**Date:** 2026-08-05
**Status:** approved

## Problem

An upstream provider (e.g. a gateway) often supports multiple API wire formats
(OpenAI chat completions, Anthropic messages, etc.) at different paths. Today a
provider is modeled with exactly one `provider_type` and one `api_base_url`, so
a single upstream can only be reached through one format/address.

## Goal

Allow configuring additional API formats per provider, each with its own
independent upstream base URL.

- Client request format selects the upstream format + address.
- Existing `provider_type` + `api_base_url` remain the **default** endpoint
  (backward compatible; used for model sync, provider test, and fallback).
- One shared API key per provider (unchanged).

## Data Model

Migration **v12** adds:

```sql
CREATE TABLE provider_endpoints (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
  api_type TEXT NOT NULL,   -- openai | anthropic | lmstudio | ollama | gemini
  base_url TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(provider_id, api_type)
);
CREATE INDEX idx_provider_endpoints_provider_id ON provider_endpoints(provider_id);
```

`providers` table is unchanged. `provider_endpoints` rows are the *additional*
formats; the primary type/URL live on `providers` as today.

## Go Model / API

- `models.ProviderEndpoint{ APIType string; BaseURL string }` (`json:"api_type"`, `json:"base_url"`)
- `models.Provider.Endpoints []ProviderEndpoint` (`json:"endpoints,omitempty"`)
- `CreateProviderRequest.Endpoints []ProviderEndpoint`
- `UpdateProviderRequest.Endpoints *[]ProviderEndpoint`

ProviderService:
- `List` loads all endpoints in one query and groups by `provider_id`.
- `GetByKey` / `GetByID` load endpoints for the single provider.
- `Create` / `Update` persist endpoints via delete-all + insert (provider-scoped rewrite).

## Proxy Endpoint Resolution

New resolver:

```go
func (e *Engine) effectiveProvider(provider *models.Provider, format apiFormat) *models.Provider
```

- Returns a shallow **copy** of the provider.
- If a matching endpoint exists, the copy's `ProviderType` and `APiBaseURL` are
  overridden with the endpoint's `api_type` / `base_url`.
- No match → default (original `provider_type` / `api_base_url`), preserving
  current cross-format translation behavior.

`apiFormat` is determined at each entry point:

| Entry | Format | Endpoint match priority |
|---|---|---|
| `HandleChatCompletions` (`/v1/chat/completions`) | openai | openai → lmstudio → ollama |
| `HandleResponses` (`/v1/responses`) | openai | openai → lmstudio → ollama |
| `HandleMessages` (`/v1/messages`) | anthropic | anthropic |
| `HandlePathRouted` (path-routed) | derived from path | chat/completions, responses, `api/*` → openai family; messages, `v1beta/*` → anthropic; else default |

`resolveDispatch` gains a format parameter. The four entry handlers pass the
resolved copy downstream. All downstream logic (adapter selection, auth
headers, gemini streaming URL, `stream_options` injection, cache/token
extraction, cost) reads `ProviderType` / `APiBaseURL` from the copy and thus
needs no changes.

## Frontend

`ProvidersView.vue` dialog:

- Keep the existing default type + base URL fields.
- Add an "additional formats" section: dynamic rows of (api_type select +
  base_url input + remove button) with an add-row button.
- Payload includes `endpoints`.

`frontend/src/stores/providers.ts` + `locales/{en,ja,ko}.ts` updated for the new
field.

## Sync / Test

Unchanged — both use the provider default (`provider_type` + `api_base_url`).

## Testing

- Unit test for `effectiveProvider` resolution (openai family priority,
  anthropic match, missing-endpoint fallback).
- Service test: endpoint save/load round-trip on provider create/update.

## Out of Scope

- Per-format API keys (single shared key per provider).
- Per-format model sync / test.
- Duplicate same-type endpoints (prevented by `UNIQUE(provider_id, api_type)`).
