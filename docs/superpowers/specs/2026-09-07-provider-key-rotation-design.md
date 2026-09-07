# Provider Upstream Key Rotation Design

**Date:** 2026-09-07
**Status:** proposed

## Problem

A provider stores exactly one encrypted upstream API key
(`providers.api_key_encrypted`). Editing the provider can replace that key, but
there is no way to keep several keys, spread load across them, or fail over
when one key is rate-limited, rejected, or the upstream is down.

## Goal

One provider can hold many upstream API keys. Proxy traffic round-robins across
active keys and, on failure, retries the remaining keys before the first
response byte is written to the client. Keys that return 401/403 are
deactivated automatically.

## Non-goals

- Persisting the round-robin cursor to SQLite
- Cooldown / auto-reactivate
- Per-key usage or cost stats
- Per-format keys (`provider_endpoints` stay URL-only)
- Gateway (`om-ni-...`) API key rotation

## Data model

Migration **v14**:

```sql
CREATE TABLE provider_api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
  api_key_encrypted TEXT NOT NULL,
  key_prefix TEXT NOT NULL,
  is_active INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_provider_api_keys_provider_id ON provider_api_keys(provider_id);
```

Copy existing keys:

```sql
INSERT INTO provider_api_keys (provider_id, api_key_encrypted, key_prefix, is_active)
SELECT id, api_key_encrypted, '', 1
FROM providers
WHERE api_key_encrypted IS NOT NULL AND api_key_encrypted != '';
```

`key_prefix` for copied rows may be empty (ciphertext is not reversible in SQL).
New keys store the first 8 characters of the plaintext.

`providers.api_key_encrypted` is **not** dropped (SQLite). It remains a
write-through copy on create and a **read fallback** only when the child table
has no rows for that provider (keeps existing test INSERTs working).

Child table is the source of truth whenever it has rows.

## Go model / admin API

```go
type ProviderAPIKeyPublic struct {
    ID        int64     `json:"id"`
    KeyPrefix string    `json:"key_prefix"`
    IsActive  bool      `json:"is_active"`
    CreatedAt time.Time `json:"created_at"`
}
```

`Provider` gains `APIKeys []ProviderAPIKeyPublic` (`json:"api_keys,omitempty"`).
Plaintext and ciphertext never leave the admin API.

| Method | Path | Behavior |
|---|---|---|
| POST | `/admin/providers` | Unchanged: one `api_key` becomes the first child row (and still written to `providers.api_key_encrypted`) |
| GET | `/admin/providers` | Includes `api_keys` (id, prefix, is_active, created_at) |
| PUT | `/admin/providers/:id` | Non-empty `api_key` **inserts** another active child row (does not delete existing keys) |
| POST | `/admin/providers/:id/keys` | Body `{ "api_key": "..." }` — encrypt, prefix, insert active |
| PATCH | `/admin/providers/:id/keys/:kid` | Body `{ "is_active": bool }` |
| DELETE | `/admin/providers/:id/keys/:kid` | Delete that row |

Manual deactivate or delete of the **last remaining active key** returns 400.
Auto-disable on 401/403 may leave zero active keys (all keys are dead; next
request fails).

CORS `AllowMethods` must include `PATCH`.

List/Get load keys in one query grouped by `provider_id`, same pattern as
`provider_endpoints`.

## Proxy

`ProviderService.ListActiveKeys(providerID) ([]providerKey, error)` returns
active child rows, or a single synthetic row from `providers.api_key_encrypted`
when the child table is empty.

Round-robin cursor is an in-memory `map[int64]uint64` plus mutex on
`ProviderService` (or `Engine`). Restart reshuffles order.

```text
# ponytail: in-memory RR cursor, persist to SQLite if multi-process ever matters
```

One helper (used by `executeUpstream`, `handlePathRoutedProxy`, and
`executeMessages` — not only the chat path):

1. Load active keys. If none, fail as today (decrypt/missing key).
2. Start at `cursor++ % n`. Try each key at most once.
3. Build the upstream request with that plaintext key; call existing
   `doUpstream` / `client.Do`.
4. Retry (close unused body, next key) when:
   - transport/network error
   - HTTP 401, 403, 429, or 5xx
5. On 401 or 403: `UPDATE provider_api_keys SET is_active=0 WHERE id=?`
   (even if it is the last active key).
6. Stop on 2xx/4xx-other (e.g. 400) and return that response.
7. If every key fails, return the **last** error/response to the client
   (same shape as today). Do not write a client error until the loop is done.

Retry happens **before** any byte is written to the Gin writer, including
streaming: stream copy starts only after a successful status is chosen.
Empty 2xx bodies are not a failover trigger (existing empty-body handling
stays).

`resolveDispatch` / `HandlePathRouted` must stop decrypting a single key and
passing it down. Decrypt happens inside the retry helper.

`FetchModelsFromProvider` and `TestProvider` use the **first active** key
only. No failover on sync/test.

## Frontend

`ProvidersView.vue` edit dialog: list of keys (prefix, active switch, delete)
plus “add key” field. Create dialog stays a single `api_key` input.

`stores/providers.ts` + `locales/{en,ja,ko}.ts` for the new endpoints and copy.

## Error handling

- Decrypt failure on one row: skip that row, try the next; if all fail, 500
  “failed to decrypt provider key”.
- Unknown provider key id / wrong provider: 404.
- Empty `api_key` on POST: 400.

Log usage only for the **terminal** attempt (the response actually returned
to the client). Attempts that failover to another key are not written to
`usage_logs`.

## Testing

- Migration v14 copies `providers.api_key_encrypted` into `provider_api_keys`.
- Two active keys: consecutive proxy calls use different keys (RR).
- Upstream 429 on first key: second key is used; client sees success.
- Upstream 401 on a key: that row `is_active=0`; next call skips it.
- All keys fail (network or 5xx): client gets one error after the loop.
- DELETE/PATCH of the last active key: 400.
- Provider with only `providers.api_key_encrypted` and no child rows still
  proxies (fallback).

Place tests next to implementations (`provider_service_test.go`, proxy tests).
No new dependencies.

## Files

- `backend/internal/database/migrations.go` (+ migration test)
- `backend/internal/models/provider.go`
- `backend/internal/service/provider_service.go`
- `backend/internal/handlers/providers.go`
- `backend/cmd/server/main.go` (routes + CORS PATCH)
- `backend/internal/proxy/upstream.go`, `proxy.go`
- `frontend/src/views/ProvidersView.vue`
- `frontend/src/stores/providers.ts`
- `frontend/src/locales/{en,ja,ko}.ts`
