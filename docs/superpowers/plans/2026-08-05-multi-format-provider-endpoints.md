# Multi-Format Provider Endpoints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a provider carry additional per-format API endpoints (api_type + base_url), and route each incoming request format to the matching upstream format/address.

**Architecture:** A new `provider_endpoints` child table holds the additional formats. `providers.provider_type` + `providers.api_base_url` remain the default endpoint (used for sync, test, and fallback). At each proxy entry point a small resolver (`effectiveProvider`) returns a shallow provider copy whose `ProviderType`/`APiBaseURL` are overridden by the request-format-matched endpoint, so all downstream logic (adapter, auth headers, gemini URL, stream_options, token/cache extraction, cost) works unchanged.

**Tech Stack:** Go 1.25 (Gin, modernc.org/sqlite), Vue 3 + Vuetify 3 + Pinia + vue-i18n (Bun).

**Spec:** `docs/superpowers/specs/2026-08-05-multi-format-provider-endpoints-design.md`

## Global Constraints

- Backend verify: `go vet ./...` and `go test ./...` from `backend/` MUST pass before a task is complete.
- Frontend verify: `bun run build` (runs `vue-tsc --noEmit`) from `frontend/` MUST pass.
- Go: `gofmt` tabs; short package names. Frontend: two-space indentation.
- Allowable api_type values: `openai`, `anthropic`, `lmstudio`, `ollama`, `gemini` (`custom` excluded — endpoints are for real upstreams only).
- Migration must be **idempotent** via `CREATE TABLE IF NOT EXISTS` (SQLite has no `ADD COLUMN IF NOT EXISTS` for the table itself).
- Existing uncommitted working-tree changes in the repo are unrelated — do NOT stage or commit anything outside the files listed in each task.
- Commit style: `feat: ...` / `test: ...` (see `git log --oneline`).

---

### Task 1: Database migration v12 + Go model types

**Files:**
- Modify: `backend/internal/database/migrations.go`
- Modify: `backend/internal/models/provider.go`
- Test: `backend/internal/database/migrations_test.go`

**Interfaces:**
- Produces: `provider_endpoints` table (columns `id`, `provider_id`, `api_type`, `base_url`, `created_at`, `updated_at`; `UNIQUE(provider_id, api_type)`; FK to `providers(id)` ON DELETE CASCADE).
- Produces: `models.ProviderEndpoint{ APIType string; BaseURL string }`, `models.Provider.Endpoints []ProviderEndpoint`, `CreateProviderRequest.Endpoints []ProviderEndpoint`, `UpdateProviderRequest.Endpoints *[]ProviderEndpoint`.

- [ ] **Step 1: Add migration v12**

Append to the `migrations` slice in `migrations.go`, after the version-11 entry:

```go
	{
		version: 12,
		up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS provider_endpoints (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
				api_type TEXT NOT NULL,
				base_url TEXT NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(provider_id, api_type)
			)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_provider_endpoints_provider_id ON provider_endpoints(provider_id)`); err != nil {
				return err
			}
			return nil
		},
	},
```

- [ ] **Step 2: Write the failing migration test**

Add to `backend/internal/database/migrations_test.go`. The existing `openTestDB(t)` helper opens a raw DB without running migrations, so call `runMigrations` explicitly:

```go
func TestMigrationV12CreatesProviderEndpoints(t *testing.T) {
	db := openTestDB(t)
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	rows, err := db.Query(`SELECT name FROM pragma_table_info('provider_endpoints')`)
	if err != nil {
		t.Fatalf("query pragma: %v", err)
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	for _, want := range []string{"provider_id", "api_type", "base_url"} {
		if !cols[want] {
			t.Errorf("provider_endpoints missing column %q (got %v)", want, cols)
		}
	}
}
```

`openTestDB` is already defined in `migrations_test.go` (uses `sql.Open` with `_foreign_keys=on`, no migration run).

- [ ] **Step 3: Run the migration test to see it fail**

Run: `go test ./internal/database/ -run TestMigrationV12CreatesProviderEndpoints -v`
Expected: FAIL (table does not exist yet).

- [ ] **Step 4: Run migration + test to see it pass**

After Step 1 is in place, re-run:
Run: `go test ./internal/database/ -run TestMigrationV12CreatesProviderEndpoints -v`
Expected: PASS.

- [ ] **Step 5: Add Go model types**

In `backend/internal/models/provider.go`:

```go
type ProviderEndpoint struct {
	APIType string `json:"api_type"`
	BaseURL string `json:"base_url"`
}
```

Add `Endpoints []ProviderEndpoint` to `Provider` (after `SourceModels`):

```go
	SourceModels      []string            `json:"source_models,omitempty"`
	Endpoints         []ProviderEndpoint  `json:"endpoints,omitempty"`
```

Add `Endpoints` to the request structs:

```go
	SourceModels    []string            `json:"source_models"`
	Endpoints       []ProviderEndpoint  `json:"endpoints"`
	ShowInModelList *bool               `json:"show_in_model_list"`
```

and

```go
	SourceModels    *[]string           `json:"source_models"`
	Endpoints       *[]ProviderEndpoint `json:"endpoints"`
	ShowInModelList *bool               `json:"show_in_model_list"`
```

- [ ] **Step 6: Commit**

```bash
git add backend/internal/database/migrations.go backend/internal/database/migrations_test.go backend/internal/models/provider.go
git commit -m "feat: provider_endpoints schema and model types"
```

---

### Task 2: ProviderService — endpoint persistence

**Files:**
- Modify: `backend/internal/service/provider_service.go`
- Test: `backend/internal/service/provider_service_test.go` (create)

**Interfaces:**
- Consumes: `models.ProviderEndpoint`, `models.Provider.Endpoints`, request structs from Task 1.
- Produces: `ProviderService` now populates `Endpoints` on `List`, `GetByKey`, `GetByID`, and persists them on `Create`/`Update`.
- Constants produced: a package-level `var endpointTypes = map[string]bool{...}` used for validation.

- [ ] **Step 1: Write the failing service test**

Create `backend/internal/service/provider_service_test.go`. It shares the package with `model_service_test.go`, so the existing `seedProvider`/`int64Ptr` helpers are available. FK is ON (`_foreign_keys=on`), so insert a `users` row before using `user_id = 1`:

```go
package service

import (
	"path/filepath"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/models"
)

func newTestProviderService(t *testing.T) *ProviderService {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES ('u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return NewProviderService(db, &config.Config{EncryptKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
}

func TestProviderEndpointsRoundTrip(t *testing.T) {
	svc := newTestProviderService(t)

	created, err := svc.Create(models.CreateProviderRequest{
		ProviderKey:  "gw",
		Name:         "Gateway",
		APiBaseURL:   "https://default.example/v1",
		APIKey:       "sk-1",
		ProviderType: "openai",
		Endpoints: []models.ProviderEndpoint{
			{APIType: "anthropic", BaseURL: "https://anthropic.example"},
			{APIType: "ollama", BaseURL: "http://ollama.local"},
		},
	}, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(created.Endpoints))
	}
	if created.Endpoints[0].APIType != "anthropic" || created.Endpoints[0].BaseURL != "https://anthropic.example" {
		t.Fatalf("endpoints[0] = %+v", created.Endpoints[0])
	}

	// GetByID loads endpoints back
	got, err := svc.GetByID(created.ID, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Endpoints) != 2 {
		t.Fatalf("get endpoints = %d, want 2", len(got.Endpoints))
	}

	// Update replaces the endpoint set (delete + insert)
	upd := []models.ProviderEndpoint{{APIType: "gemini", BaseURL: "https://gemini.example"}}
	_, err = svc.Update(created.ID, 1, models.UpdateProviderRequest{Endpoints: &upd})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	final, _ := svc.GetByID(created.ID, 1)
	if len(final.Endpoints) != 1 || final.Endpoints[0].APIType != "gemini" {
		t.Fatalf("after update endpoints = %+v", final.Endpoints)
	}
}

func TestProviderEndpointsRejectsInvalidType(t *testing.T) {
	svc := newTestProviderService(t)
	_, err := svc.Create(models.CreateProviderRequest{
		ProviderKey:  "gw",
		Name:         "Gateway",
		APiBaseURL:   "https://default.example",
		APIKey:       "sk-1",
		ProviderType: "openai",
		Endpoints:    []models.ProviderEndpoint{{APIType: "bogus", BaseURL: "https://x.example"}},
	}, 1)
	if err == nil {
		t.Fatal("expected error for invalid api_type")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/service/ -run 'TestProviderEndpoints' -v`
Expected: FAIL (`undefined: saveEndpoints` or "endpoints = 0").

- [ ] **Step 3: Implement endpoint persistence**

In `provider_service.go` add a validation constant and a loader helper:

```go
var endpointTypes = map[string]bool{
	"openai": true, "anthropic": true, "lmstudio": true, "ollama": true, "gemini": true,
}
```

```go
func (s *ProviderService) loadEndpoints(p *models.Provider) error {
	rows, err := s.db.Query(`SELECT api_type, base_url FROM provider_endpoints WHERE provider_id = ? ORDER BY id`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ep models.ProviderEndpoint
		if err := rows.Scan(&ep.APIType, &ep.BaseURL); err != nil {
			return err
		}
		p.Endpoints = append(p.Endpoints, ep)
	}
	return rows.Err()
}
```

Add the saver helper:

```go
func (s *ProviderService) saveEndpoints(providerID int64, endpoints []models.ProviderEndpoint) error {
	for _, ep := range endpoints {
		if !endpointTypes[ep.APIType] {
			return &ProviderError{Message: "invalid endpoint api_type: " + ep.APIType, StatusCode: 400}
		}
		if ep.BaseURL == "" {
			return &ProviderError{Message: "endpoint base_url is required", StatusCode: 400}
		}
	}
	if _, err := s.db.Exec(`DELETE FROM provider_endpoints WHERE provider_id = ?`, providerID); err != nil {
		return err
	}
	for _, ep := range endpoints {
		if _, err := s.db.Exec(
			`INSERT INTO provider_endpoints (provider_id, api_type, base_url) VALUES (?, ?, ?)`,
			providerID, ep.APIType, ep.BaseURL,
		); err != nil {
			return err
		}
	}
	return nil
}
```

Wire into `List` (after the `for i := range providers` loop), `GetByKey` (before `return &p, nil`), and `GetByID` (before `return &p, nil`):

```go
	if err := s.loadEndpoints(&p); err != nil {
		return nil, err
	}
```

For `List`, call `s.loadEndpoints(&providers[i])` inside the existing loop (append before/after `loadSourceModels`):

```go
	for i := range providers {
		if providers[i].ProviderType == "custom" {
			s.loadSourceModels(&providers[i])
		}
		if err := s.loadEndpoints(&providers[i]); err != nil {
			return nil, err
		}
	}
```

Wire into `Create`: after the INSERT succeeds, capture the new id, then reject endpoints for custom and save:

```go
	id, _ := result.LastInsertId()
	if req.ProviderType == "custom" && len(req.Endpoints) > 0 {
		return nil, &ProviderError{Message: "endpoints are not supported for custom providers", StatusCode: 400}
	}
	if err := s.saveEndpoints(id, req.Endpoints); err != nil {
		return nil, err
	}

	provider, err := s.GetByID(id, userID)
	if err != nil {
		return nil, err
	}
```

Note: `result.LastInsertId()` currently exists as `id, _ := result.LastInsertId()` in `Create` — keep that line and insert the two custom-check + save lines right after it (before `provider, err := s.GetByID(id, userID)`).

Wire into `Update`: after the UPDATE SQL runs (both branches) and after the custom source-model reimport block, add:

```go
	if req.Endpoints != nil {
		if providerType == "custom" && len(*req.Endpoints) > 0 {
			return nil, &ProviderError{Message: "endpoints are not supported for custom providers", StatusCode: 400}
		}
		if err := s.saveEndpoints(id, *req.Endpoints); err != nil {
			return nil, err
		}
	}
```

- [ ] **Step 4: Run the service tests to make them pass**

Run: `go test ./internal/service/ -run 'TestProviderEndpoints' -v`
Expected: PASS.

- [ ] **Step 5: Run all backend tests + vet**

Run: `go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/provider_service.go backend/internal/service/provider_service_test.go
git commit -m "feat: persist per-format provider endpoints"
```

---

### Task 3: Proxy — `effectiveProvider` resolver

**Files:**
- Modify: `backend/internal/proxy/proxy_helpers.go`
- Test: `backend/internal/proxy/proxy_helpers_test.go`

**Interfaces:**
- Produces: `type apiFormat int`, constants `apiFormatOpenAI`, `apiFormatAnthropic`, and `func effectiveProvider(provider *models.Provider, format apiFormat) *models.Provider`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/proxy/proxy_helpers_test.go`:

```go
func TestEffectiveProvider(t *testing.T) {
	base := &models.Provider{
		ProviderKey:  "gw",
		ProviderType: "openai",
		APiBaseURL:   "https://default.example/v1",
		Endpoints: []models.ProviderEndpoint{
			{APIType: "anthropic", BaseURL: "https://anthropic.example"},
		},
	}

	// OpenAI-family request falls back to the default (no openai/lmstudio/ollama endpoint)
	if got := effectiveProvider(base, apiFormatOpenAI); got != base {
		t.Errorf("openai family: expected original provider, got %+v", got)
	}

	// Anthropic request uses the anthropic endpoint
	got := effectiveProvider(base, apiFormatAnthropic)
	if got == base {
		t.Fatal("anthropic family: expected a copy, got original")
	}
	if got.ProviderType != "anthropic" || got.APiBaseURL != "https://anthropic.example" {
		t.Errorf("anthropic copy = %s / %s", got.ProviderType, got.APiBaseURL)
	}
	// Original is unchanged
	if base.ProviderType != "openai" || base.APiBaseURL != "https://default.example/v1" {
		t.Errorf("original mutated: %s / %s", base.ProviderType, base.APiBaseURL)
	}
}

func TestEffectiveProviderOpenAIPriority(t *testing.T) {
	p := &models.Provider{
		ProviderType: "gemini",
		APiBaseURL:   "https://gemini.example",
		Endpoints: []models.ProviderEndpoint{
			{APIType: "ollama", BaseURL: "http://ollama.local"},
			{APIType: "openai", BaseURL: "https://openai.example"},
		},
	}
	got := effectiveProvider(p, apiFormatOpenAI)
	if got.ProviderType != "openai" || got.APiBaseURL != "https://openai.example" {
		t.Errorf("openai priority = %s / %s", got.ProviderType, got.APiBaseURL)
	}
}

func TestEffectiveProviderNoEndpoints(t *testing.T) {
	p := &models.Provider{ProviderType: "openai", APiBaseURL: "https://default.example/v1"}
	if got := effectiveProvider(p, apiFormatAnthropic); got != p {
		t.Errorf("no endpoints: expected original, got %+v", got)
	}
}
```

Check that `models` is imported in `proxy_helpers_test.go` (add `"omnirelay/internal/models"` if missing).

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/proxy/ -run 'TestEffectiveProvider' -v`
Expected: FAIL (`undefined: effectiveProvider`).

- [ ] **Step 3: Implement the resolver**

Append to `proxy_helpers.go`:

```go
// apiFormat identifies the incoming request's wire format, used to pick a
// matching upstream endpoint.
type apiFormat int

const (
	apiFormatOpenAI apiFormat = iota
	apiFormatAnthropic
)

// openaiCompatPriority is the endpoint api_type preference for OpenAI-family
// requests (mirrors isOpenAICompat).
var openaiCompatPriority = []string{"openai", "lmstudio", "ollama"}

// effectiveProvider returns a shallow copy of provider whose ProviderType and
// APiBaseURL are overridden by the endpoint matching the request format, or the
// original provider when no matching endpoint exists.
func effectiveProvider(provider *models.Provider, format apiFormat) *models.Provider {
	types := formatEndpointTypes(format)
	for _, t := range types {
		for i := range provider.Endpoints {
			if provider.Endpoints[i].APIType == t {
				cp := *provider
				cp.ProviderType = t
				cp.APiBaseURL = provider.Endpoints[i].BaseURL
				return &cp
			}
		}
	}
	return provider
}

func formatEndpointTypes(format apiFormat) []string {
	if format == apiFormatAnthropic {
		return []string{"anthropic"}
	}
	return openaiCompatPriority
}
```

- [ ] **Step 4: Run the tests to make them pass**

Run: `go test ./internal/proxy/ -run 'TestEffectiveProvider' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/proxy/proxy_helpers.go backend/internal/proxy/proxy_helpers_test.go
git commit -m "feat: resolve per-format provider endpoint in proxy"
```

---

### Task 4: Wire the resolver into all entry points

**Files:**
- Modify: `backend/internal/proxy/proxy.go`
- Modify: `backend/internal/proxy/responses.go`

**Interfaces:**
- Consumes: `effectiveProvider`, `apiFormatOpenAI`, `apiFormatAnthropic` from Task 3.
- `resolveDispatch` signature becomes `func (e *Engine) resolveDispatch(c *gin.Context, fullModelID string, userID int64, format apiFormat) (*models.Model, *models.Provider, Adapter, string, bool)`.

- [ ] **Step 1: Change `resolveDispatch` signature + apply resolver**

In `proxy.go`, the current declaration is:

```go
func (e *Engine) resolveDispatch(c *gin.Context, fullModelID string, userID int64, errFmt apiresponse.Format) (*models.Model, *models.Provider, Adapter, string, bool) {
```

Change it to add a trailing `format apiFormat` parameter (keep `errFmt apiresponse.Format` for error formatting — it is unchanged):

```go
func (e *Engine) resolveDispatch(c *gin.Context, fullModelID string, userID int64, errFmt apiresponse.Format, format apiFormat) (*models.Model, *models.Provider, Adapter, string, bool) {
```

Inside the body, right before the adapter selection (currently `adapter := e.getAdapter(provider.ProviderType)`), apply the resolver:

```go
	provider = effectiveProvider(provider, format)
```

Edit all three callers to append the new apiFormat argument:
- `proxy.go:32` (HandleChatCompletions): add `apiFormatOpenAI` after `apiresponse.FormatOpenAI`
- `responses.go:178` (HandleResponses): add `apiFormatOpenAI` after `apiresponse.FormatOpenAI`
- `proxy.go:56` (HandleMessages): add `apiFormatAnthropic` after `apiresponse.FormatAnthropic`

Each call becomes, e.g. for HandleMessages:

```go
	dbModel, provider, adapter, apiKey, ok := e.resolveDispatch(c, fullModelID, userID, apiresponse.FormatAnthropic, apiFormatAnthropic)
```

- [ ] **Step 2: Apply resolver in `HandlePathRouted`**

In `proxy.go` `HandlePathRouted`:
1. Remove the line `adapter := e.getAdapter(provider.ProviderType)` (currently ~line 182).
2. After the existing `isMessages` definition (~line 192) and before the `if isChatCompletions && adapter != nil ...` branch (~line 207), insert:

```go
	format := apiFormatOpenAI
	if isMessages || strings.HasPrefix(apiPrefix, "v1beta") {
		format = apiFormatAnthropic
	}
	provider = effectiveProvider(provider, format)
	adapter := e.getAdapter(provider.ProviderType)
```

`strings` is already imported in `proxy.go`.

- [ ] **Step 3: Run all backend tests + vet**

Run: `go vet ./... && go test ./...`
Expected: all PASS (this validates the wiring did not break existing OpenAI/Anthropic/path-routed flows).

- [ ] **Step 4: Write an end-to-end routing test**

Add to `backend/internal/proxy/proxy_helpers_test.go`:

```go
func TestMultiFormatRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	openaiUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"openai"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	t.Cleanup(openaiUpstream.Close)

	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"anthropic"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(anthropicUpstream.Close)

	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'gw', 'Gateway', ?, ?, 'openai', 1)`,
		openaiUpstream.URL, encryptTestAPIKey(t, "sk-test"),
	); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO provider_endpoints (provider_id, api_type, base_url) VALUES (1, 'anthropic', ?)`,
		anthropicUpstream.URL,
	); err != nil {
		t.Fatalf("seed endpoint: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'm', 'gw', 1, 1)`,
	); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	providerSvc := service.NewProviderService(db, &config.Config{EncryptKey: testEncryptKey})
	modelSvc := service.NewModelService(db)
	usageSvc := service.NewUsageService(db)
	engine := NewEngine(providerSvc, modelSvc, usageSvc, nil, nil)

	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleChatCompletions(c)
	})
	r.POST("/v1/messages", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleMessages(c)
	})

	// OpenAI-family request → no openai/lmstudio/ollama endpoint, falls back to
	// the provider default (openaiUpstream).
	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gw/m","messages":[{"role":"user","content":"hi"}]}`))
	chatReq.Header.Set("Content-Type", "application/json")
	chatW := httptest.NewRecorder()
	r.ServeHTTP(chatW, chatReq)
	if chatW.Code != http.StatusOK {
		t.Fatalf("chat status = %d, body = %s", chatW.Code, chatW.Body.String())
	}
	if !strings.Contains(chatW.Body.String(), "openai") {
		t.Fatalf("chat body = %s", chatW.Body.String())
	}

	// Anthropic request → /v1/messages routes to the anthropic endpoint.
	msgReq := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"gw/m","messages":[{"role":"user","content":"hi"}]}`))
	msgReq.Header.Set("Content-Type", "application/json")
	msgW := httptest.NewRecorder()
	r.ServeHTTP(msgW, msgReq)
	if msgW.Code != http.StatusOK {
		t.Fatalf("messages status = %d, body = %s", msgW.Code, msgW.Body.String())
	}
	if !strings.Contains(msgW.Body.String(), "anthropic") {
		t.Fatalf("messages body = %s", msgW.Body.String())
	}
}
```

This test requires imports already present or to be added to the test file: `net/http/httptest`, `io`, `strings`, `path/filepath`, `omnirelay/internal/config`, `omnirelay/internal/database`, `omnirelay/internal/service`, `github.com/gin-gonic/gin`. Verify `TestMultiFormatRouting` PASSes (this proves the resolver is actually used by `/v1/messages`).

- [ ] **Step 5: Run all backend tests + vet again**

Run: `go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/proxy/proxy.go backend/internal/proxy/responses.go backend/internal/proxy/proxy_helpers_test.go
git commit -m "feat: route requests to per-format provider endpoints"
```

---

### Task 5: Frontend — per-format endpoints form

**Files:**
- Modify: `frontend/src/views/ProvidersView.vue`
- Modify: `frontend/src/stores/providers.ts`
- Modify: `frontend/src/locales/en.ts`, `frontend/src/locales/ja.ts`, `frontend/src/locales/ko.ts`

**Interfaces:**
- Payload shape: `endpoints: { api_type: string; base_url: string }[]`.

- [ ] **Step 1: Add store types**

In `frontend/src/stores/providers.ts` add to the `Provider` interface:

```ts
  endpoints?: { api_type: string; base_url: string }[];
```

and to `CreateProviderPayload`:

```ts
  endpoints?: { api_type: string; base_url: string }[];
```

- [ ] **Step 2: Add locale keys**

`en.ts` providers block, after `showInModelListHint`:

```ts
    additionalFormats: "Additional API formats",
    additionalFormatsHint: "Route a request format to its own upstream address (optional)",
    addFormat: "Add format",
```

`ja.ts` providers block, after `showInModelListHint`:

```ts
    additionalFormats: "追加のAPI形式",
    additionalFormatsHint: "リクエスト形式を独自のアップストリームアドレスにルーティング（任意）",
    addFormat: "形式を追加",
```

`ko.ts` providers block, after `showInModelListHint`:

```ts
    additionalFormats: "추가 API 형식",
    additionalFormatsHint: "요청 형식을 각자의 업스트림 주소로 라우팅 (선택)",
    addFormat: "형식 추가",
```

- [ ] **Step 3: Add form state**

In `ProvidersView.vue` script, add an `endpointTypes` constant next to `providerTypes`:

```ts
const endpointTypes = ["openai", "anthropic", "lmstudio", "ollama", "gemini"];
```

Add `endpoints` to the `form` ref default:

```ts
  source_models: [] as string[],
  endpoints: [] as { api_type: string; base_url: string }[],
```

In `openDialog`, for the edit branch after `source_models: provider.source_models ?? [],` add:

```ts
      endpoints: provider.endpoints ? provider.endpoints.map((e: any) => ({ ...e })) : [],
```

and for the create branch after `source_models: [],` add:

```ts
      endpoints: [],
```

- [ ] **Step 4: Add the form UI**

In the template, insert this block after the provider type `<select>` field group (the block at lines 183-192) and before the `custom` source-models block:

```html
          <div v-if="form.provider_type !== 'custom'" class="field-group">
            <label class="field-label">{{
              $t("providers.additionalFormats")
            }}</label>
            <p class="field-hint">{{ $t("providers.additionalFormatsHint") }}</p>
            <div
              v-for="(ep, i) in form.endpoints"
              :key="i"
              class="endpoint-row"
            >
              <select v-model="ep.api_type" class="field-select endpoint-select">
                <option v-for="t in endpointTypes" :key="t" :value="t">
                  {{ t }}
                </option>
              </select>
              <input
                v-model="ep.base_url"
                class="field-input"
                placeholder="https://..."
              />
              <button
                class="row-btn row-btn--danger"
                :title="$t('common.delete')"
                @click="form.endpoints.splice(i, 1)"
              >
                <v-icon size="15">mdi-close</v-icon>
              </button>
            </div>
            <button
              type="button"
              class="btn-secondary"
              @click="form.endpoints.push({ api_type: 'openai', base_url: '' })"
            >
              <v-icon size="14">mdi-plus</v-icon>
              {{ $t("providers.addFormat") }}
            </button>
          </div>
```

Add scoped styles at the end of the `<style scoped>` block:

```css
.endpoint-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}
.endpoint-row .endpoint-select {
  flex: 0 0 120px;
  min-width: 0;
}
.endpoint-row .field-input {
  flex: 1 1 auto;
}
.btn-secondary {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--chip-bg, #eee);
  border: 1px solid var(--border, #ccc);
  border-radius: 6px;
  padding: 4px 10px;
  font-size: 13px;
  cursor: pointer;
}
```

The remove button uses `$t("common.delete")` — that key exists in all three locales (see the `common` block, e.g. `en.ts:7`).

- [ ] **Step 5: Ensure `endpoints` is sent in the payload**

In `handleSave`, both the edit and create branches already send the whole spread `form.value` (edit strips `auto_sync`/`source_models`, create sends all). Confirm `endpoints` survives the destructuring in the edit branch:

```ts
      const rest = form.value.provider_type === "custom"
        ? (({ auto_sync: _s, ...r }) => r)(form.value)
        : (({ source_models: _o, auto_sync: _s, ...r }) => r)(form.value);
```

This keeps `endpoints` in `rest` for non-custom providers. For `custom`, `endpoints` is empty anyway. No change needed here unless the destructure excludes it — it does not.

- [ ] **Step 6: Type-check + build**

Run: `bun run build` (from `frontend/`)
Expected: `vue-tsc --noEmit` passes (watch for unused vars — the `title` attr on the remove button uses a locale key; make sure the key exists).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/views/ProvidersView.vue frontend/src/stores/providers.ts frontend/src/locales/en.ts frontend/src/locales/ja.ts frontend/src/locales/ko.ts
git commit -m "feat: per-format API path editor in provider dialog"
```

---

### Task 6: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Backend**

From `backend/`:
Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: `gofmt -l` prints nothing; vet and all tests pass.

- [ ] **Step 2: Frontend**

From `frontend/`:
Run: `bun run build`
Expected: type check + production build succeed.

- [ ] **Step 3: Manual sanity (if a live environment is available)**

With the server running:
1. Create (or edit) a provider `gw`, default type `openai`, default URL `https://default.example/v1`, and add an `anthropic` endpoint pointing at a real Anthropic-compatible gateway.
2. `POST /v1/messages` with `{ "model": "gw/<model>", "messages": [...] }` → reaches the anthropic endpoint.
3. `POST /v1/chat/completions` with the same model → reaches the default openai endpoint.

- [ ] **Step 4: Final commit (if any stray changes remain from verification)**

```bash
git status --short   # confirm only intended files are modified
```

## Out of Scope (from spec)

- Per-format API keys (single shared key per provider).
- Per-format model sync / test (uses the provider default).
- Duplicate same-type endpoints (prevented by `UNIQUE(provider_id, api_type)`).
- `api_type: custom` endpoints (rejected by service validation).
