# WebSocket Real-Time Updates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add WebSocket-based real-time usage tracking to the Dashboard, Usage, and Logs pages. New proxy requests instantly appear in admin views without manual refresh.

**Architecture:** A hub (`internal/hub/`) manages WebSocket connections keyed by userID. After persisting usage, the proxy Engine broadcasts events to connected admins. The frontend (`frontend/src/api/ws.ts`) connects via `/admin/ws`, dispatches events to the Pinia usage store which updates reactive state.

**Tech Stack:** Go 1.25, gorilla/websocket v1.5.3, Vue 3, Pinia, axios

---

### Task 1: Create the Hub package (Go)

**Files:**
- Create: `backend/internal/hub/hub.go`

- [ ] **Step 1: Write the hub implementation**

```go
package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Conn represents a single WebSocket connection tied to one user.
type Conn struct {
	hub    *Hub
	userID int64
	done   chan struct{}
	mu     sync.Mutex
	events chan []byte
	once   sync.Once
}

func (c *Conn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *Conn) Send(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return fmt.Errorf("connection closed")
	case c.events <- payload:
		return nil
	}
}

func (c *Conn) Ping() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return fmt.Errorf("connection closed")
	default:
		return nil
	}
}

// Hub maintains active connections per user and routes events.
type Hub struct {
	mu          sync.Mutex
	connections map[int64][]*Conn
}

func New() *Hub {
	return &Hub{connections: make(map[int64][]*Conn)}
}

// Register adds a Conn to the hub for the given userID.
func (h *Hub) Register(userID int64) *Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &Conn{
		hub:    h,
		userID: userID,
		done:   make(chan struct{}, 1),
		events: make(chan []byte, 16),
	}
	h.connections[userID] = append(h.connections[userID], c)
	return c
}

// Unregister removes a Conn from the hub.
func (h *Hub) Unregister(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.connections[c.userID]
	for i, conn := range conns {
		if conn == c {
			h.connections[c.userID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(h.connections[c.userID]) == 0 {
		delete(h.connections, c.userID)
	}
}

// Broadcast sends an event to all connections for the given userID.
// Failed writes are silently dropped; cleanup happens on next write attempt.
func (h *Hub) Broadcast(userID int64, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.connections[userID]
	for _, c := range conns {
		c.mu.Lock()
		select {
		case <-c.done:
			c.mu.Unlock()
			go h.Unregister(c)
			continue
		default:
		}
		select {
		case c.events <- data:
		default:
			close(c.done)
			go h.Unregister(c)
		}
		c.mu.Unlock()
	}
}

// ReadPump handles incoming messages (pings/close) from the client.
func (c *Conn) ReadPump(conn *websocket.Conn) {
	defer func() {
		c.once.Do(func() { close(c.done) })
		c.hub.Unregister(c)
		conn.Close()
	}()
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

// WritePump sends queued events to the WebSocket.
// Returns when done channel is signaled or write fails.
func (c *Conn) WritePump(conn *websocket.Conn) {
	for {
		select {
		case <-c.done:
			conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		case payload := <-c.events:
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				c.once.Do(func() { close(c.done) })
				c.hub.Unregister(c)
				return
			}
		}
	}
}
```

- [ ] **Step 2: Commit**

```bash
cd backend && go mod tidy
git add -A
git commit -m "feat(hub): add WebSocket hub for real-time user-scoped broadcasts"
```

---

### Task 2: Hub unit tests

**Files:**
- Create: `backend/internal/hub/hub_test.go`

- [ ] **Step 1: Write hub tests**

```go
package hub

import (
	"encoding/json"
	"testing"
)

func TestHubRegisterBroadcast(t *testing.T) {
	h := New()
	c := h.Register(1)

	event := Event{Type: "test", Data: json.RawMessage(`{"hello":"world"}`)}
	h.Broadcast(1, event)

	select {
	case got := <-c.events:
		var e Event
		if err := json.Unmarshal(got, &e); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if e.Type != "test" {
			t.Errorf("expected type 'test', got '%s'", e.Type)
		}
	default:
		t.Fatal("expected event on channel")
	}
}

func TestHubUserIsolation(t *testing.T) {
	h := New()
	c1 := h.Register(1)
	c2 := h.Register(2)

	h.Broadcast(1, Event{Type: "msg", Data: json.RawMessage(`"only for user 1"`)})

	select {
	case <-c1.events:
		// expected
	default:
		t.Fatal("user 1 should have received event")
	}

	select {
	case <-c2.events:
		t.Fatal("user 2 should NOT have received event")
	default:
	}
}

func TestHubMultiConn(t *testing.T) {
	h := New()
	c1 := h.Register(1)
	c2 := h.Register(1)

	h.Broadcast(1, Event{Type: "multi", Data: json.RawMessage(`true`)})

	received := 0
	select {
	case <-c1.events:
		received++
	default:
	}
	select {
	case <-c2.events:
		received++
	default:
	}
	if received != 2 {
		t.Fatalf("expected 2 deliveries, got %d", received)
	}

	_ = c1
	_ = c2
}

func TestHubUnregister(t *testing.T) {
	h := New()
	c := h.Register(1)
	h.Unregister(c)

	h.Broadcast(1, Event{Type: "gone", Data: json.RawMessage(`true`)})

	h.mu.Lock()
	conns := h.connections[1]
	h.mu.Unlock()
	if len(conns) != 0 {
		t.Fatalf("expected 0 connections after unregister, got %d", len(conns))
	}
}
```

- [ ] **Step 2: Run tests**

```bash
cd backend && go test ./internal/hub/... -v
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "test(hub): add hub unit tests for register, broadcast, isolation"
```

---

### Task 3: Wire Hub into Proxy Engine

**Files:**
- Modify: `backend/internal/proxy/adapter.go`
- Modify: `backend/internal/proxy/usage_log.go`

- [ ] **Step 1: Add `hub` field to Engine struct**

In `backend/internal/proxy/adapter.go`, change:

```go
import (
	"net/http"
	"omnirelay/internal/hub"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
)

type Engine struct {
	providerService *service.ProviderService
	modelService    *service.ModelService
	usageService    *service.UsageService
	hub             *hub.Hub
	httpClient      *http.Client
	adapters        map[string]Adapter
}

func NewEngine(ps *service.ProviderService, ms *service.ModelService, us *service.UsageService, h *hub.Hub) *Engine {
	e := &Engine{
		providerService: ps,
		modelService:    ms,
		usageService:    us,
		hub:             h,
		httpClient:      &http.Client{Timeout: upstreamRequestTimeout},
		adapters:        make(map[string]Adapter),
	}
	// ... rest unchanged
}
```

- [ ] **Step 2: Import fmt in hub.go (was missing in Task 1)**

In `backend/internal/hub/hub.go`, add `"fmt"` to imports. (The `Conn.Send` method uses `fmt.Errorf`.)

- [ ] **Step 3: Add broadcast calls in usage_log.go**

In `backend/internal/proxy/usage_log.go`, modify `logTokenUsage` and `logUpstreamError` to broadcast:

```go
func (e *Engine) logTokenUsage(u usageContext, t tokenUsage) {
	entry := u.base()
	entry.RequestTokens = t.requestTokens
	entry.ResponseTokens = t.responseTokens
	if t.totalTokens > 0 {
		entry.TotalTokens = t.totalTokens
	} else {
		entry.TotalTokens = t.requestTokens + t.responseTokens
	}
	entry.CacheWrite5MTokens = t.cacheWrite5m
	entry.CacheWrite1HTokens = t.cacheWrite1h
	entry.CacheReadTokens = t.cacheRead
	entry.Cost = t.cost
	entry.LatencyMs = t.latencyMs
	entry.StartedAt = t.startedAt
	entry.CompletedAt = t.completedAt
	e.persistUsage(entry)
	e.broadcastUsage(u, entry, t)
}

func (e *Engine) logUpstreamError(u usageContext, message string, latencyMs int64) {
	entry := u.base()
	entry.IsError = true
	entry.ErrorMessage = message
	entry.LatencyMs = latencyMs
	e.persistUsage(entry)
	e.broadcastUsage(u, entry, tokenUsage{})
}

func (e *Engine) broadcastUsage(u usageContext, entry models.UsageLog, t tokenUsage) {
	if e.hub == nil {
		return
	}

	// Build usage_log event
	logData, err := json.Marshal(map[string]interface{}{
		"id":                     entry.ID,
		"model":                  entry.Model,
		"request_tokens":         entry.RequestTokens,
		"response_tokens":        entry.ResponseTokens,
		"total_tokens":           entry.TotalTokens,
		"cache_write_5m_tokens":  entry.CacheWrite5MTokens,
		"cache_write_1h_tokens":  entry.CacheWrite1HTokens,
		"cache_read_tokens":      entry.CacheReadTokens,
		"latency_ms":             entry.LatencyMs,
		"cost":                   entry.Cost,
		"is_error":               entry.IsError,
		"completed_at":           entry.CompletedAt,
		"created_at":             entry.CreatedAt,
	})
	if err != nil {
		log.Printf("failed to marshal usage_log event: %v", err)
		return
	}
	e.hub.Broadcast(u.userID, hub.Event{Type: "usage_log", Data: logData})

	// Build stats_delta event
	go func() {
		stats, err := e.usageService.GetStats(u.userID)
		if err != nil {
			log.Printf("failed to get stats for broadcast: %v", err)
			return
		}
		statsData, err := json.Marshal(map[string]interface{}{
			"today_cost":      stats.TodayCost,
			"today_requests":  stats.TodayRequests,
			"today_tokens":    stats.TodayTokens,
			"total_cost":      stats.TotalCost,
			"total_requests":  stats.TotalRequests,
			"total_tokens":    stats.TotalTokens,
			"rpm":            stats.RPM,
			"tpm":            stats.TPM,
			"avg_latency_ms": stats.AvgLatencyMs,
		})
		if err != nil {
			log.Printf("failed to marshal stats_delta event: %v", err)
			return
		}
		e.hub.Broadcast(u.userID, hub.Event{Type: "stats_delta", Data: statsData})
	}()
}
```

Add imports to `usage_log.go`:
```go
import (
	"encoding/json"
	"log"
	"omnirelay/internal/hub"
	"omnirelay/internal/models"
	"time"
)
```

- [ ] **Step 4: Run backend tests**

```bash
cd backend && go test ./...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(proxy): broadcast usage events via hub after persist"
```

---

### Task 4: Create WebSocket upgrade handler

**Files:**
- Create: `backend/internal/handlers/ws.go`

- [ ] **Step 1: Write the WS handler**

```go
package handlers

import (
	"net/http"
	"omnirelay/internal/hub"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func WebSocketUpgrader(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := c.Query("token")
		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid claims"})
			return
		}
		userID := int64(claims["user_id"].(float64))

		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}

		// Note: Hub is not passed directly; instead use package-level reference
		// or restructure. For simplicity we use a package-level setHub approach.
		hubConn := currentHub.Register(userID)

		go hubConn.WritePump(conn)
		hubConn.ReadPump(conn)
	}
}

// currentHub is set at startup. This avoids changing the handler signature.
var currentHub *hub.Hub

func SetHub(h *hub.Hub) {
	currentHub = h
}
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "feat(handlers): add JWT-authenticated WebSocket upgrade handler"
```

---

### Task 5: Wire hub into main.go

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add imports and wire hub**

In `backend/cmd/server/main.go`:

Add import:
```go
"omnirelay/internal/hub"
```

After `proxyEngine := proxy.NewEngine(...)` block, before `r := gin.Default()`:

```go
h := hub.New()
handlers.SetHub(h)
proxyEngine := proxy.NewEngine(providerService, modelService, usageService, h)
```

Register route inside `adminAuth` block:

```go
adminAuth.GET("/ws", handlers.WebSocketUpgrader(cfg.JWTSecret))
```

- [ ] **Step 2: Add gorilla/websocket dependency**

```bash
cd backend && go get github.com/gorilla/websocket@v1.5.3 && go mod tidy
```

- [ ] **Step 3: Run backend build**

```bash
cd backend && go build ./...
```

Expected: no errors

- [ ] **Step 4: Run all tests**

```bash
cd backend && go test ./...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: wire WebSocket hub into server startup and proxy engine"
```

---

### Task 6: Create frontend WebSocket client module

**Files:**
- Create: `frontend/src/api/ws.ts`

- [ ] **Step 1: Write the WS client**

```typescript
import axios from "axios";
import { useAuthStore } from "../stores/auth";

export interface UsageLogEntry {
  id: number;
  model: string;
  request_tokens: number;
  response_tokens: number;
  total_tokens: number;
  cache_write_5m_tokens: number;
  cache_write_1h_tokens: number;
  cache_read_tokens: number;
  latency_ms: number;
  cost: number;
  is_error: boolean;
  completed_at: string | null;
  created_at: string;
}

export interface StatsDelta {
  today_cost: number;
  today_requests: number;
  today_tokens: number;
  total_cost: number;
  total_requests: number;
  total_tokens: number;
  rpm: number;
  tpm: number;
  avg_latency_ms: number;
}

type EventHandler = (data: any) => void;

export interface RealtimeConnection {
  onUsageLog: (cb: (entry: UsageLogEntry) => void) => void;
  onStatsDelta: (cb: (delta: StatsDelta) => void) => void;
  close: () => void;
  isConnected: () => boolean;
}

export function createRealtimeConnection(): RealtimeConnection {
  const auth = useAuthStore();
  let ws: WebSocket | null = null;
  let reconnectDelay = 1000;
  let closed = false;
  let connected = false;

  const usageLogHandlers: EventHandler[] = [];
  const statsDeltaHandlers: EventHandler[] = [];

  function connect() {
    if (!auth.token || closed) return;

    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const url = `${proto}://${window.location.host}/admin/ws?token=${auth.token}`;
    ws = new WebSocket(url);

    ws.onopen = () => {
      connected = true;
      reconnectDelay = 1000;
      // Resync on reconnect
      axios.get("/admin/stats").then((res) => {
        statsDeltaHandlers.forEach((cb) => cb(res.data));
      });
    };

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === "usage_log") {
          usageLogHandlers.forEach((cb) => cb(msg.data));
        } else if (msg.type === "stats_delta") {
          statsDeltaHandlers.forEach((cb) => cb(msg.data));
        }
      } catch (e) {
        console.warn("Failed to parse WS message:", e);
      }
    };

    ws.onclose = () => {
      connected = false;
      if (!closed) {
        setTimeout(connect, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, 30000);
      }
    };

    ws.onerror = () => {
      ws?.close();
    };
  }

  connect();

  return {
    onUsageLog(cb) {
      usageLogHandlers.push(cb);
    },
    onStatsDelta(cb) {
      statsDeltaHandlers.push(cb);
    },
    close() {
      closed = true;
      ws?.close();
      ws = null;
    },
    isConnected() {
      return connected;
    },
  };
}
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "feat(frontend): add WebSocket realtime client with auto-reconnect"
```

---

### Task 7: Extend usage store with real-time support

**Files:**
- Modify: `frontend/src/stores/usage.ts`

- [ ] **Step 1: Add connection state and event handlers**

Add import at top:
```typescript
import { createRealtimeConnection, UsageLogEntry, StatsDelta } from "../api/ws";
```

Add to store function body (after `const error = ref<string | null>(null)`):

```typescript
let rtConn: ReturnType<typeof createRealtimeConnection> | null = null;

function connect() {
  if (rtConn) return;
  rtConn = createRealtimeConnection();
  rtConn.onUsageLog((entry: UsageLogEntry) => {
    logs.value.unshift(entry);
    if (logs.value.length > 50) logs.value.pop();
    total.value += 1;
  });
  rtConn.onStatsDelta((delta: StatsDelta) => {
    stats.value = {
      ...stats.value,
      total_requests: delta.total_requests,
      total_tokens: delta.total_tokens,
      total_cost: delta.total_cost,
      avg_latency_ms: delta.avg_latency_ms,
      today_cost: delta.today_cost,
      today_requests: delta.today_requests,
      today_tokens: delta.today_tokens,
      rpm: delta.rpm,
      tpm: delta.tpm,
    };
  });
}

function disconnect() {
  rtConn?.close();
  rtConn = null;
}

function isRealtimeConnected(): boolean {
  return rtConn?.isConnected() ?? false;
```

Add to the return statement:
```typescript
return { stats, logs, total, loading, error, fetchStats, fetchLogs, clearError, connect, disconnect, isRealtimeConnected };
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "feat(store): integrate WebSocket realtime into usage store"
```

---

### Task 8: Connect DashboardView to realtime

**Files:**
- Modify: `frontend/src/views/DashboardView.vue`

- [ ] **Step 1: Add lifecycle hooks**

In the `<script setup>` block, change `onMounted` to:

```typescript
import { onUnmounted } from "vue";

onMounted(() => {
  usageStore.fetchStats();
  usageStore.connect();
});

onUnmounted(() => {
  usageStore.disconnect();
});
```

- [ ] **Step 2: Run type check**

```bash
cd frontend && bun run build 2>&1 | head -30
```

Expected: no type errors

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat(dashboard): connect to realtime WebSocket on mount"
```

---

### Task 9: Connect UsageView to realtime

**Files:**
- Modify: `frontend/src/views/UsageView.vue`

- [ ] **Step 1: Add lifecycle hooks**

In the `<script setup>` block:

```typescript
import { onMounted, onUnmounted } from "vue";

onMounted(() => {
  store.fetchStats();
  store.connect();
});

onUnmounted(() => {
  store.disconnect();
});
```

- [ ] **Step 2: Run type check**

```bash
cd frontend && bun run build 2>&1 | head -30
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat(usage): connect to realtime WebSocket on mount"
```

---

### Task 10: Connect LogsView to realtime

**Files:**
- Modify: `frontend/src/views/LogsView.vue`

- [ ] **Step 1: Add lifecycle hooks**

In the `<script setup>` block, change `onMounted`:

```typescript
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  loadLogs();
  store.connect();
});

onUnmounted(() => {
  window.removeEventListener("resize", checkMobile);
  store.disconnect();
});
```

- [ ] **Step 2: Run type check**

```bash
cd frontend && bun run build 2>&1 | head -30
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat(logs): connect to realtime WebSocket for live log entries"
```

---

### Task 11: Full verification

- [ ] **Step 1: Run all backend tests**

```bash
cd backend && go test ./... && go vet ./...
```

Expected: PASS, no vet warnings

- [ ] **Step 2: Run frontend build**

```bash
cd frontend && bun run build
```

Expected: success, exit code 0

- [ ] **Step 3: Final commit check**

```bash
git log --oneline -15
```

Expected: commits for hub, tests, proxy integration, handler, wiring, frontend WS client, store, three view files
