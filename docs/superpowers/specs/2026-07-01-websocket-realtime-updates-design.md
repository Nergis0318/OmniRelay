# WebSocket Real-Time Dashboard Updates

**Date**: 2026-07-01
**Status**: Approved

## Overview

Add WebSocket-based real-time updates to the Dashboard, Usage, and Logs pages. When a proxy request completes, connected admin clients instantly see the new usage log entry and updated aggregate stats without manual refresh.

## Architecture

```
Client (Browser)                          Backend
┌──────────────────┐    WS /admin/ws     ┌──────────────────┐
│ DashboardView    │◄───────────────────►│ Hub (hub.go)     │
│ UsageView        │   JWT in query      │  - connections   │
│ LogsView         │                     │    map[userID]   │
│                  │                     │    []*Conn       │
│ usageStore (Pinia)                     │  - Broadcast()   │
│  - applyEvent()  │                     │                  │
└──────────────────┘                     │ Proxy Engine     │
                                         │  - logTokenUsage │────► Hub.Broadcast()
                                         │  - logUpstreamErr│────► Hub.Broadcast()
                                         └──────────────────┘
```

## Backend Changes

### 1. `internal/hub/hub.go` (new)

- `Hub` struct with `map[int64][]*Conn`, mutex-protected
- `Subscribe(userID int64, conn *Conn)` / `Unsubscribe(userID int64, conn *Conn)`
- `Broadcast(userID int64, event Event)` — sends JSON to all conns for that user; removes dead conns
- `Conn` wraps `*websocket.Conn` with a write mutex and `done` channel
- `Event` struct: `Type string` + `Data json.RawMessage`

### 2. `internal/hub/hub_test.go` (new)

- Test broadcast fan-out to multiple conns for same user
- Test isolation: user A events don't reach user B
- Test unsubscribe on write failure

### 3. `internal/proxy/adapter.go` — Engine struct

Add `hub *hub.Hub` field. Update `NewEngine` signature to accept it.

### 4. `internal/proxy/usage_log.go` — Broadcast after persist

After `persistUsage()` succeeds in `logTokenUsage` and `logUpstreamError`, call `h.Broadcast()` with:
- `usage_log` event containing the new log entry
- `stats_delta` event containing updated aggregate counters (today_cost, today_requests, today_tokens, total_cost, total_requests, total_tokens, rpm, tpm)

### 5. `internal/handlers/ws.go` (new)

- `WebSocketUpgrader(hub *hub.Hub, jwtSecret string) gin.HandlerFunc`
- Extract JWT from `?token=` query param
- Validate with same logic as JWTAuth middleware
- Upgrade using gorilla websocket.Upgrader (check Origin for security)
- Register with hub, start read pump (handle close/ping)
- On unregister: close conn, remove from hub

### 6. `cmd/server/main.go`

- Import `github.com/gorilla/websocket`
- Create hub instance, pass to `proxy.NewEngine`
- Register route: `adminAuth.GET("/ws", handlers.WebSocketUpgrader(hub, cfg.JWTSecret))`

### 7. `go.mod` / `go.sum`

Add `github.com/gorilla/websocket v1.5.3` dependency.

## Frontend Changes

### 1. `frontend/src/api/ws.ts` (new)

- `createRealtimeConnection(token: string): RealtimeConnection`
- Returns object with `onUsageLog(cb)`, `onStatsDelta(cb)`, `close()`
- Auto-reconnect with exponential backoff (1s → 2s → 4s → ... → 30s cap)
- On reconnect: re-fetch `/stats` to resync state
- Parse incoming JSON messages, dispatch by `type` field

### 2. `frontend/src/stores/usage.ts` — WS integration

- `connect()` — opens WS using auth token, wires events to store
- `disconnect()` — cleanup on component unmount
- `applyUsageLog(entry)` — prepend to `logs` (cap at 50 for display), increment `total`
- `applyStatsDelta(delta)` — update `stats` counters (today_cost, today_requests, today_tokens, total_cost, total_requests, total_tokens, rpm, tpm)

### 3. `frontend/src/views/DashboardView.vue`

- Call `usageStore.connect()` on mount, `disconnect()` on unmount
- RPM/TPM update live as events arrive
- Chart animates when daily_usage data changes

### 4. `frontend/src/views/UsageView.vue`

- Call `usageStore.connect()` on mount, `disconnect()` on unmount
- Stat cards update live
- Chart animates with new data

### 5. `frontend/src/views/LogsView.vue`

- Call `usageStore.connect()` on mount, `disconnect()` on unmount
- New log entries appear at top of table in real-time (prepend to array)
- Pagination offset adjusts to account for prepended items

## Event Message Format

### `usage_log` event
```json
{
  "type": "usage_log",
  "data": {
    "id": 123,
    "model": "openai/gpt-4o",
    "request_tokens": 150,
    "response_tokens": 300,
    "total_tokens": 450,
    "cache_write_5m_tokens": 0,
    "cache_write_1h_tokens": 0,
    "cache_read_tokens": 0,
    "latency_ms": 1200,
    "cost": 0.0023,
    "is_error": false,
    "provider_name": "OpenAI",
    "completed_at": "2026-07-01T12:34:56Z"
  }
}
```

### `stats_delta` event
```json
{
  "type": "stats_delta",
  "data": {
    "today_cost": 1.2345,
    "today_requests": 42,
    "today_tokens": 50000,
    "total_cost": 123.45,
    "total_requests": 1500,
    "total_tokens": 2000000,
    "rpm": 8.5,
    "tpm": 12000,
    "avg_latency_ms": 1450
  }
}
```

## Error Handling

| Scenario | Behavior |
|----------|----------|
| WS write fails | Close + unsubscribe that connection |
| Client detects close | Reconnect with exponential backoff |
| On reconnect | Re-fetch `/stats` to resync |
| Server ping timeout (60s) | Close stale connection |
| Invalid JWT on WS upgrade | Return 401, no upgrade |
| JWT expired during session | Client detects 401 close → re-auth → reconnect |

## Security

- JWT validated before upgrade (same secret as REST auth)
- Origin check on WebSocket upgrade (reject cross-origin)
- User-scoped broadcasts: user A never receives user B's events
- No sensitive data (API keys) in WS messages

## Testing

- `go test ./internal/hub/...` — hub unit tests
- `go test ./...` — full backend test suite
- `bun run build` — frontend type check
- Manual: open dashboard, make a proxy request, verify live update
