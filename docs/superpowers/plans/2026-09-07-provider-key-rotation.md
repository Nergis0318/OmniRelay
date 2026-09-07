# Provider Upstream Key Rotation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One provider can hold many upstream API keys; proxy traffic round-robins across active keys and failovers on network/401/403/429/5xx before any client byte is written; 401/403 auto-deactivates that key.

**Architecture:** New `provider_api_keys` table is the source of truth. `providers.api_key_encrypted` stays (SQLite) as create write-through plus read fallback when the child table is empty. Round-robin cursor is in-memory on `ProviderService`. One `Engine.tryKeys` helper is used by `executeUpstream`, `proxyJSONRequest`/`executeMessages`, and `handlePathRoutedProxy`.

**Tech Stack:** Go 1.25, Gin, SQLite (`modernc.org/sqlite`), Vue 3 + Pinia. Tests: `go test` from `backend/`. Frontend check: `bun run build` from `frontend/`.

**Spec:** `docs/superpowers/specs/2026-09-07-provider-key-rotation-design.md`

## Global Constraints

- No new dependencies.
- Do not drop `providers.api_key_encrypted`.
- Child table is source of truth whenever it has rows; fallback only when it has none.
- Retry before any Gin write, including streaming. Empty 2xx is not failover.
- Log `usage_logs` only for the terminal attempt.
- Manual delete/deactivate of the last active key → 400. Auto-disable on 401/403 may leave zero active keys.
- CORS must allow PATCH.
- In-memory RR cursor (do not persist). Mark with `// ponytail: in-memory RR cursor, persist to SQLite if multi-process ever matters`.
- `FetchModelsFromProvider` / `TestProvider` use the first active key only (no failover, do not advance RR).
- PUT `/providers/:id` with non-empty `api_key` **inserts** another child row (does not replace).
- Do not use `rg` in any shell. Use bun for frontend, go from `backend/`.
- Do not commit unless the user explicitly asks; skip Commit steps if not asked.

---

### Task 1: Migration v14 `provider_api_keys`

**Files:**
- Modify: `backend/internal/database/migrations.go` (append after v13, currently ends at line 324)
- Test: `backend/internal/database/migrations_test.go`

**Interfaces:**
- Consumes: existing `migrations []migration`, `runMigrations`, `openTestDB`
- Produces: table `provider_api_keys (id, provider_id, api_key_encrypted, key_prefix, is_active, created_at)` plus copy of existing `providers.api_key_encrypted` rows (`key_prefix` empty for copied rows)

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/database/migrations_test.go`:

```go
func TestMigrationV14CopiesProviderKeys(t *testing.T) {
	db := openTestDB(t)

	for _, m := range migrations {
		if m.version >= 14 {
			break
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := m.up(tx); err != nil {
			tx.Rollback()
			t.Fatalf("migration v%d: %v", m.version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES ('u', 'h')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES ('openai', 'OpenAI', 'https://api.openai.com/v1', 'enc-abc', 'openai', 1)`,
	); err != nil {
		t.Fatal(err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='provider_api_keys'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("provider_api_keys missing")
	}

	var pid int64
	var enc, prefix string
	var active int
	if err := db.QueryRow(`SELECT provider_id, api_key_encrypted, key_prefix, is_active FROM provider_api_keys`).Scan(&pid, &enc, &prefix, &active); err != nil {
		t.Fatalf("copied row: %v", err)
	}
	if enc != "enc-abc" || active != 1 || prefix != "" {
		t.Fatalf("got enc=%q prefix=%q active=%d", enc, prefix, active)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestMigrationV14CopiesProviderKeys -v`
Expected: FAIL (`version >= 14` never applies, `provider_api_keys` missing)

Workdir: `backend/`

- [ ] **Step 3: Add migration v14**

In `migrations.go`, append after the v13 entry (before the closing `}` of `var migrations`):

```go
{
	version: 14,
	up: func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS provider_api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
			api_key_encrypted TEXT NOT NULL,
			key_prefix TEXT NOT NULL DEFAULT '',
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_provider_api_keys_provider_id ON provider_api_keys(provider_id)`); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO provider_api_keys (provider_id, api_key_encrypted, key_prefix, is_active)
			SELECT id, api_key_encrypted, '', 1 FROM providers
			WHERE api_key_encrypted IS NOT NULL AND api_key_encrypted != ''`); err != nil {
			return err
		}
		return nil
	},
},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run 'TestMigrationV14CopiesProviderKeys|TestRunMigrations' -v`
Expected: PASS (`TestRunMigrationsFreshDatabase` still asserts `count == len(migrations)`)

- [ ] **Step 5: Commit** (skip unless the user asked)

```bash
git add backend/internal/database/migrations.go backend/internal/database/migrations_test.go
git commit -m "feat: add provider_api_keys table and migrate existing keys"
```

---

### Task 2: ProviderService key CRUD, RR cursor, fallback

**Files:**
- Modify: `backend/internal/models/provider.go`
- Modify: `backend/internal/service/provider_service.go`
- Test: `backend/internal/service/provider_service_test.go`

**Interfaces:**
- Consumes: `crypto.Encrypt` / `Decrypt`, `database.Init` (v14 present)
- Produces:

```go
type ProviderAPIKeyPublic struct {
	ID        int64     `json:"id"`
	KeyPrefix string    `json:"key_prefix"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// on models.Provider:
APIKeys []ProviderAPIKeyPublic `json:"api_keys,omitempty"`

type UpstreamKey struct {
	ID        int64
	Plaintext string
}

func keyPrefix(plaintext string) string

func (s *ProviderService) ListActiveKeys(provider *models.Provider) ([]UpstreamKey, error)
func (s *ProviderService) FirstActiveKey(provider *models.Provider) (UpstreamKey, error)
func (s *ProviderService) NextStartIndex(providerID int64, n int) int
func (s *ProviderService) DeactivateKey(id int64) error
func (s *ProviderService) AddKey(providerID, userID int64, plaintext string) (*models.ProviderAPIKeyPublic, error)
func (s *ProviderService) SetKeyActive(providerID, keyID, userID int64, active bool) error
func (s *ProviderService) DeleteKey(providerID, keyID, userID int64) error
```

`NewProviderService` must init `rr map[int64]uint64`. `Create` inserts the first child row when `encrypted != ""`. `Update` with non-empty `api_key` inserts another child row (still encrypts; do not delete existing child rows). `List` / `GetByID` / `GetByKey` call `loadAPIKeys`. `FetchModelsFromProvider` uses `FirstActiveKey` instead of `DecryptAPIKey(provider.APIKeyEncrypted)`.

- [ ] **Step 1: Write the failing tests**

Append to `provider_service_test.go`:

```go
func TestCreateProviderWritesChildKey(t *testing.T) {
	svc := newTestProviderService(t)
	p, err := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-abcdefghi", ProviderType: "openai",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.APIKeys) != 1 || p.APIKeys[0].KeyPrefix != "sk-abcde" || !p.APIKeys[0].IsActive {
		t.Fatalf("api_keys = %+v", p.APIKeys)
	}
	keys, err := svc.ListActiveKeys(p)
	if err != nil || len(keys) != 1 || keys[0].Plaintext != "sk-abcdefghi" {
		t.Fatalf("active = %+v err=%v", keys, err)
	}
}

func TestAddKeyAndRoundRobin(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "key-one-xx", ProviderType: "openai",
	}, 1)
	if _, err := svc.AddKey(p.ID, 1, "key-two-yy"); err != nil {
		t.Fatal(err)
	}
	p, _ = svc.GetByID(p.ID, 1)
	keys, err := svc.ListActiveKeys(p)
	if err != nil || len(keys) != 2 {
		t.Fatalf("want 2 keys, got %+v err=%v", keys, err)
	}
	i0 := svc.NextStartIndex(p.ID, 2)
	i1 := svc.NextStartIndex(p.ID, 2)
	if i0 == i1 {
		t.Fatalf("RR did not advance: %d %d", i0, i1)
	}
}

func TestDeleteLastActiveKeyRejected(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-1", ProviderType: "openai",
	}, 1)
	err := svc.DeleteKey(p.ID, p.APIKeys[0].ID, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ProviderError)
	if !ok || pe.StatusCode != 400 {
		t.Fatalf("got %v", err)
	}
}

func TestSetKeyActiveLastRejected(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-1", ProviderType: "openai",
	}, 1)
	err := svc.SetKeyActive(p.ID, p.APIKeys[0].ID, 1, false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFallbackWhenNoChildRows(t *testing.T) {
	svc := newTestProviderService(t)
	enc, err := encryptForTest(t, svc, "sk-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(
		`INSERT INTO providers (provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES ('legacy', 'L', 'https://example/v1', ?, 'openai', 1)`, enc,
	); err != nil {
		t.Fatal(err)
	}
	p, err := svc.GetByKey("legacy", 1)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := svc.ListActiveKeys(p)
	if err != nil || len(keys) != 1 || keys[0].Plaintext != "sk-fallback" {
		t.Fatalf("fallback = %+v err=%v", keys, err)
	}
}

func TestUpdateAPIKeyInsertsAnother(t *testing.T) {
	svc := newTestProviderService(t)
	p, _ := svc.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-one", ProviderType: "openai",
	}, 1)
	k2 := "sk-two"
	got, err := svc.Update(p.ID, 1, models.UpdateProviderRequest{APIKey: &k2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.APIKeys) != 2 {
		t.Fatalf("want 2 keys, got %+v", got.APIKeys)
	}
}

func encryptForTest(t *testing.T, svc *ProviderService, plain string) (string, error) {
	t.Helper()
	return svc.DecryptAPIKey("") // placeholder — implementer: use crypto.Encrypt(plain, svc.cfg.EncryptKey)
}
```

Replace `encryptForTest` with a real helper using `omnirelay/internal/crypto`:

```go
func encryptForTest(t *testing.T, svc *ProviderService, plain string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plain, svc.cfg.EncryptKey)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}
```

And change the fallback test to `enc := encryptForTest(...)`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -run 'TestCreateProviderWritesChildKey|TestAddKeyAndRoundRobin|TestDeleteLastActiveKeyRejected|TestSetKeyActiveLastRejected|TestFallbackWhenNoChildRows|TestUpdateAPIKeyInsertsAnother' -v`
Expected: FAIL (types/methods missing)

- [ ] **Step 3: Minimal implementation**

`models/provider.go` — add:

```go
type ProviderAPIKeyPublic struct {
	ID        int64     `json:"id"`
	KeyPrefix string    `json:"key_prefix"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}
```

Add field on `Provider`:

```go
APIKeys []ProviderAPIKeyPublic `json:"api_keys,omitempty"`
```

`provider_service.go`:

- Add imports `"sync"` if not present.
- Struct:

```go
type ProviderService struct {
	db  *sql.DB
	cfg *config.Config
	rrMu sync.Mutex
	rr   map[int64]uint64
}

func NewProviderService(db *sql.DB, cfg *config.Config) *ProviderService {
	return &ProviderService{db: db, cfg: cfg, rr: make(map[int64]uint64)}
}
```

```go
func keyPrefix(plaintext string) string {
	if len(plaintext) <= 8 {
		return plaintext
	}
	return plaintext[:8]
}

type UpstreamKey struct {
	ID        int64
	Plaintext string
}

func (s *ProviderService) loadAPIKeys(p *models.Provider) error {
	rows, err := s.db.Query(
		`SELECT id, key_prefix, is_active, created_at FROM provider_api_keys WHERE provider_id = ? ORDER BY id`,
		p.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k models.ProviderAPIKeyPublic
		if err := rows.Scan(&k.ID, &k.KeyPrefix, &k.IsActive, &k.CreatedAt); err != nil {
			return err
		}
		p.APIKeys = append(p.APIKeys, k)
	}
	return rows.Err()
}

func (s *ProviderService) insertKey(providerID int64, plaintext string) error {
	enc, err := crypto.Encrypt(plaintext, s.cfg.EncryptKey)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO provider_api_keys (provider_id, api_key_encrypted, key_prefix, is_active) VALUES (?, ?, ?, 1)`,
		providerID, enc, keyPrefix(plaintext),
	)
	return err
}

func (s *ProviderService) ListActiveKeys(provider *models.Provider) ([]UpstreamKey, error) {
	rows, err := s.db.Query(
		`SELECT id, api_key_encrypted FROM provider_api_keys WHERE provider_id = ? AND is_active = 1 ORDER BY id`,
		provider.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []UpstreamKey
	for rows.Next() {
		var id int64
		var enc string
		if err := rows.Scan(&id, &enc); err != nil {
			return nil, err
		}
		plain, err := s.DecryptAPIKey(enc)
		if err != nil {
			continue
		}
		keys = append(keys, UpstreamKey{ID: id, Plaintext: plain})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 && provider.APIKeyEncrypted != "" {
		plain, err := s.DecryptAPIKey(provider.APIKeyEncrypted)
		if err != nil {
			return nil, err
		}
		return []UpstreamKey{{ID: 0, Plaintext: plain}}, nil
	}
	return keys, nil
}

func (s *ProviderService) FirstActiveKey(provider *models.Provider) (UpstreamKey, error) {
	keys, err := s.ListActiveKeys(provider)
	if err != nil {
		return UpstreamKey{}, err
	}
	if len(keys) == 0 {
		return UpstreamKey{}, &ProviderError{Message: "no active provider API keys", StatusCode: 400}
	}
	return keys[0], nil
}

func (s *ProviderService) NextStartIndex(providerID int64, n int) int {
	if n <= 0 {
		return 0
	}
	s.rrMu.Lock()
	defer s.rrMu.Unlock()
	// ponytail: in-memory RR cursor, persist to SQLite if multi-process ever matters
	v := s.rr[providerID]
	s.rr[providerID] = v + 1
	return int(v % uint64(n))
}

func (s *ProviderService) DeactivateKey(id int64) error {
	if id == 0 {
		return nil
	}
	_, err := s.db.Exec(`UPDATE provider_api_keys SET is_active = 0 WHERE id = ?`, id)
	return err
}

func (s *ProviderService) countActiveKeys(providerID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM provider_api_keys WHERE provider_id = ? AND is_active = 1`,
		providerID,
	).Scan(&n)
	return n, err
}

func (s *ProviderService) AddKey(providerID, userID int64, plaintext string) (*models.ProviderAPIKeyPublic, error) {
	if plaintext == "" {
		return nil, &ProviderError{Message: "api_key is required", StatusCode: 400}
	}
	if _, err := s.GetByID(providerID, userID); err != nil {
		return nil, err
	}
	if err := s.insertKey(providerID, plaintext); err != nil {
		return nil, err
	}
	p, err := s.GetByID(providerID, userID)
	if err != nil {
		return nil, err
	}
	if len(p.APIKeys) == 0 {
		return nil, fmt.Errorf("key insert failed")
	}
	k := p.APIKeys[len(p.APIKeys)-1]
	return &k, nil
}

func (s *ProviderService) SetKeyActive(providerID, keyID, userID int64, active bool) error {
	if _, err := s.GetByID(providerID, userID); err != nil {
		return err
	}
	if !active {
		n, err := s.countActiveKeys(providerID)
		if err != nil {
			return err
		}
		var isActive int
		err = s.db.QueryRow(
			`SELECT is_active FROM provider_api_keys WHERE id = ? AND provider_id = ?`,
			keyID, providerID,
		).Scan(&isActive)
		if err == sql.ErrNoRows {
			return &ProviderError{Message: "key not found", StatusCode: 404}
		}
		if err != nil {
			return err
		}
		if isActive == 1 && n <= 1 {
			return &ProviderError{Message: "cannot deactivate the last active key", StatusCode: 400}
		}
	}
	res, err := s.db.Exec(
		`UPDATE provider_api_keys SET is_active = ? WHERE id = ? AND provider_id = ?`,
		active, keyID, providerID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ProviderError{Message: "key not found", StatusCode: 404}
	}
	return nil
}

func (s *ProviderService) DeleteKey(providerID, keyID, userID int64) error {
	if _, err := s.GetByID(providerID, userID); err != nil {
		return err
	}
	n, err := s.countActiveKeys(providerID)
	if err != nil {
		return err
	}
	var isActive int
	err = s.db.QueryRow(
		`SELECT is_active FROM provider_api_keys WHERE id = ? AND provider_id = ?`,
		keyID, providerID,
	).Scan(&isActive)
	if err == sql.ErrNoRows {
		return &ProviderError{Message: "key not found", StatusCode: 404}
	}
	if err != nil {
		return err
	}
	if isActive == 1 && n <= 1 {
		return &ProviderError{Message: "cannot delete the last active key", StatusCode: 400}
	}
	res, err := s.db.Exec(`DELETE FROM provider_api_keys WHERE id = ? AND provider_id = ?`, keyID, providerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ProviderError{Message: "key not found", StatusCode: 404}
	}
	return nil
}
```

Wire:

- After successful `Create` insert (got `id`), if `encrypted != ""` call `s.insertKey(id, req.APIKey)`.
- In `Update`, when `req.APIKey != nil && *req.APIKey != ""`, after the existing providers UPDATE, call `s.insertKey(id, *req.APIKey)` (do not remove other child rows).
- `List` / `GetByID` / `GetByKey`: after `loadEndpoints`, `if err := s.loadAPIKeys(&p); err != nil`.
- `FetchModelsFromProvider`: replace decrypt of `provider.APIKeyEncrypted` with:

```go
uk, err := s.FirstActiveKey(provider)
if err != nil {
	return nil, err
}
apiKey := uk.Plaintext
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/ -v`
Expected: PASS

- [ ] **Step 5: Commit** (skip unless asked)

```bash
git add backend/internal/models/provider.go backend/internal/service/provider_service.go backend/internal/service/provider_service_test.go
git commit -m "feat: manage multiple provider API keys with round-robin cursor"
```

---

### Task 3: Admin HTTP — list keys, add, PATCH, DELETE

**Files:**
- Modify: `backend/internal/handlers/providers.go`
- Modify: `backend/cmd/server/main.go` (CORS PATCH; three routes)
- Test: `backend/internal/handlers/handlers_test.go`

**Interfaces:**
- Consumes: `AddKey`, `SetKeyActive`, `DeleteKey`; `ProviderError`
- Produces:

| Method | Path | Handler |
|---|---|---|
| POST | `/admin/providers/:id/keys` | `AddProviderKey` body `{"api_key":"..."}` |
| PATCH | `/admin/providers/:id/keys/:kid` | `SetProviderKeyActive` body `{"is_active":bool}` |
| DELETE | `/admin/providers/:id/keys/:kid` | `DeleteProviderKey` |

`TestProvider` uses `FirstActiveKey` instead of `DecryptAPIKey(provider.APIKeyEncrypted)`.
`UpdateProvider` must map `*service.ProviderError` like `CreateProvider` (insert-key 400 must not become 404).

- [ ] **Step 1: Write the failing handler test**

Append to `handlers_test.go`:

```go
func TestProviderKeyRoutes(t *testing.T) {
	_, ps, _, _, _, tokenStr := setupTest(t)
	p, err := ps.Create(models.CreateProviderRequest{
		ProviderKey: "oa", Name: "OA", APiBaseURL: "https://example/v1",
		APIKey: "sk-one", ProviderType: "openai",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.POST("/admin/providers/:id/keys", adminAuthMiddleware(tokenStr), AddProviderKey(ps))
	r.PATCH("/admin/providers/:id/keys/:kid", adminAuthMiddleware(tokenStr), SetProviderKeyActive(ps))
	r.DELETE("/admin/providers/:id/keys/:kid", adminAuthMiddleware(tokenStr), DeleteProviderKey(ps))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/providers/"+itoa(p.ID)+"/keys", strings.NewReader(`{"api_key":"sk-two"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/admin/providers/"+itoa(p.ID)+"/keys/"+itoa(p.APIKeys[0].ID), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete first: %d %s", w.Code, w.Body.String())
	}

	got, _ := ps.GetByID(p.ID, 1)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/admin/providers/"+itoa(p.ID)+"/keys/"+itoa(got.APIKeys[0].ID), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete last: %d %s", w.Code, w.Body.String())
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
```

Add imports `strconv` and `omnirelay/internal/models` if missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/ -run TestProviderKeyRoutes -v`
Expected: FAIL (handlers undefined)

- [ ] **Step 3: Implement handlers and routes**

Append to `providers.go`:

```go
func writeProviderErr(c *gin.Context, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		apiresponse.AbortAdminNotFound(c, "not found")
		return
	}
	var pe *service.ProviderError
	if errors.As(err, &pe) {
		apiresponse.AbortAdminError(c, pe.StatusCode, err.Error(), "")
		return
	}
	apiresponse.AbortAdminInternal(c, err.Error())
}

func AddProviderKey(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid provider ID")
			return
		}
		var req struct {
			APIKey string `json:"api_key"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		k, err := svc.AddKey(id, userID, req.APIKey)
		if err != nil {
			writeProviderErr(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"key": k})
	}
}

func SetProviderKeyActive(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid provider ID")
			return
		}
		kid, err := strconv.ParseInt(c.Param("kid"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid key ID")
			return
		}
		var req struct {
			IsActive *bool `json:"is_active"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.IsActive == nil {
			apiresponse.AbortAdminBadRequest(c, "is_active is required")
			return
		}
		if err := svc.SetKeyActive(id, kid, userID, *req.IsActive); err != nil {
			writeProviderErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func DeleteProviderKey(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid provider ID")
			return
		}
		kid, err := strconv.ParseInt(c.Param("kid"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid key ID")
			return
		}
		if err := svc.DeleteKey(id, kid, userID); err != nil {
			writeProviderErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "key deleted"})
	}
}
```

In `CreateProvider` keep existing `ProviderError` mapping. In `UpdateProvider`, replace the `AbortAdminNotFound` blanket with `writeProviderErr` (or the same `errors.As` as Create). Not-found from `GetByID` is `sql.ErrNoRows` — map that to 404, `ProviderError` to its status:

```go
provider, err := svc.Update(id, userID, req)
if err != nil {
	if errors.Is(err, sql.ErrNoRows) {
		apiresponse.AbortAdminNotFound(c, "provider not found")
		return
	}
	writeProviderErr(c, err)
	return
}
```

Add `"database/sql"` import to handlers if used. If `GetByID` wraps `sql.ErrNoRows` as a plain error string, keep 404 on `strings.Contains(err.Error(), "no rows")` **or** check `errors.Is`. Prefer: `writeProviderErr` plus:

```go
if err != nil {
	writeProviderErr(c, err)
	return
}
```

and change `GetByID` only if tests require 404 — existing Update already used NotFound for all errors. After this change, unknown errors become 500. That is correct.

`TestProvider`: replace decrypt block with:

```go
uk, err := ps.FirstActiveKey(provider)
if err != nil {
	writeProviderErr(c, err)
	return
}
apiKey := uk.Plaintext
```

`main.go`:

```go
AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
```

Inside `adminOnly` next to existing provider routes:

```go
adminOnly.POST("/providers/:id/keys", handlers.AddProviderKey(providerService))
adminOnly.PATCH("/providers/:id/keys/:kid", handlers.SetProviderKeyActive(providerService))
adminOnly.DELETE("/providers/:id/keys/:kid", handlers.DeleteProviderKey(providerService))
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/handlers/ ./cmd/server/ -v`
Expected: PASS

- [ ] **Step 5: Commit** (skip unless asked)

```bash
git add backend/internal/handlers/providers.go backend/internal/handlers/handlers_test.go backend/cmd/server/main.go
git commit -m "feat: admin API for provider key add, toggle, and delete"
```

---

### Task 4: Proxy round-robin + failover

**Files:**
- Modify: `backend/internal/proxy/upstream.go`
- Modify: `backend/internal/proxy/proxy.go` (`resolveDispatch`, `executeMessages`, `HandleMessages`, `HandlePathRouted` messages branch, `handlePathRoutedProxy`)
- Modify: `backend/internal/proxy/proxy_helpers.go` (`proxyJSONRequest`)
- Modify: `backend/internal/proxy/chat_handler.go` only if `buildAndSendChatRequest` still treats non-success as immediate client error — it must not write until `tryKeys` finishes
- Test: `backend/internal/proxy/key_failover_test.go` (create)

**Interfaces:**
- Consumes: `ListActiveKeys`, `NextStartIndex`, `DeactivateKey`
- Produces:

```go
func failoverStatus(err error, status int) bool
func (e *Engine) tryKeys(provider *models.Provider, fn func(key service.UpstreamKey) (*http.Response, time.Time, error)) (*http.Response, time.Time, error)
```

`resolveDispatch` stops decrypting. Keep the current 5-value signature and return `""` for apiKey (call sites: `HandleChatCompletions` `proxy.go:32`, `HandleMessages` `proxy.go:56`, `HandleResponses` `responses.go:179`) so those files do not churn. `executeMessages` still drops its `apiKey` parameter and uses `tryKeys` via `proxyJSONRequest`.

`proxyJSONRequest` signature becomes:

```go
func (e *Engine) proxyJSONRequest(c *gin.Context, u usageContext, provider *models.Provider, upstreamURL string, adaptedBody map[string]interface{}, logNewRequestErrors bool) (*http.Response, time.Time, bool)
```

Only caller today: `executeMessages` in `proxy.go:314`.

Usage logging: do **not** log inside the failover loop. Log only when returning the terminal failure/success to the client (existing `logErrorResponse` / `logUpstreamError` after the loop).

- [ ] **Step 1: Write the failing proxy tests**

Create `backend/internal/proxy/key_failover_test.go`. Reuse `encryptTestAPIKey`, `testEncryptKey`, and the router pattern from `interruption_test.go`. Seed **child** keys (not only `providers.api_key_encrypted`) so RR/failover is exercised.

```go
package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"omnirelay/internal/config"
	"omnirelay/internal/database"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

func seedKeyFailoverRouter(t *testing.T, upstream *httptest.Server, keys []string) (*gin.Engine, *service.ProviderService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'u', 'h')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO providers (id, provider_key, name, api_base_url, api_key_encrypted, provider_type, user_id)
		 VALUES (1, 'p', 'P', ?, ?, 'openai', 1)`,
		upstream.URL, encryptTestAPIKey(t, keys[0]),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO models (id, provider_id, model_id, provider_key, is_manual, user_id)
		 VALUES (1, 1, 'm', 'p', 1, 1)`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, key_hash, key_prefix, name, created_by) VALUES (1, 'h', 'om-ni-xxx', 'k', 1)`,
	); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{EncryptKey: testEncryptKey}
	ps := service.NewProviderService(db, cfg)
	for _, k := range keys {
		if _, err := ps.AddKey(1, 1, k); err != nil {
			t.Fatal(err)
		}
	}
	engine := NewEngine(ps, service.NewModelService(db), service.NewUsageService(db), nil, nil)
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		engine.HandleChatCompletions(c)
	})
	return r, ps
}

func chatBody() string {
	return `{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`
}

func TestKeyRoundRobin(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	r, _ := seedKeyFailoverRouter(t, upstream, []string{"key-a", "key-b"})
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("call %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if len(seen) < 2 || seen[0] == seen[1] {
		t.Fatalf("RR keys = %v", seen)
	}
}

func TestFailoverOn429(t *testing.T) {
	var n atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(429)
			io.WriteString(w, `{"error":"rate"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	r, _ := seedKeyFailoverRouter(t, upstream, []string{"key-a", "key-b"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if n.Load() < 2 {
		t.Fatalf("did not retry, hits=%d", n.Load())
	}
}

func Test401DeactivatesKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "bad-key" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"nope"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	r, ps := seedKeyFailoverRouter(t, upstream, []string{"bad-key", "good-key"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	p, _ := ps.GetByID(1, 1)
	var activeBad bool
	for _, k := range p.APIKeys {
		if k.KeyPrefix == "bad-key" && k.IsActive {
			activeBad = true
		}
	}
	if activeBad {
		t.Fatal("401 key still active")
	}
}

func TestAllKeysFailReturnsOneError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		io.WriteString(w, `{"error":"down"}`)
	}))
	defer upstream.Close()
	r, _ := seedKeyFailoverRouter(t, upstream, []string{"k1", "k2"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody()))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("expected error")
	}
	var payload map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &payload)
	if payload == nil {
		t.Fatalf("empty body %s", w.Body.String())
	}
}
```

Note: `seedKeyFailoverRouter` calls `Create`-style `AddKey` **in addition to** the providers INSERT. That yields `len(keys)+1` child rows if v14 also copied `api_key_encrypted`. To keep exactly `len(keys)` rows, **do not** insert a dummy encrypted value that becomes a third key:

- Insert `providers.api_key_encrypted` as `''` **or**
- After AddKey, delete extras **or**
- Insert provider then only AddKey, no copy duplicate: insert provider with `api_key_encrypted=''` then AddKey for each. Fallback tests already cover the column. Prefer empty encrypted + AddKey only.

Adjust seed:

```go
`INSERT INTO providers (..., api_key_encrypted, ...) VALUES (..., '', 'openai', 1)`
```

then `AddKey` for each plaintext.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy/ -run 'TestKeyRoundRobin|TestFailoverOn429|Test401DeactivatesKey|TestAllKeysFailReturnsOneError' -v`
Expected: FAIL (single key / no retry)

- [ ] **Step 3: Implement tryKeys and wire callers**

Add to `upstream.go` (or `proxy_helpers.go` if you want it next to `doUpstream` — prefer `upstream.go`):

```go
func failoverStatus(err error, status int) bool {
	if err != nil {
		return true
	}
	switch status {
	case 401, 403, 429:
		return true
	}
	return status >= 500
}

func (e *Engine) tryKeys(provider *models.Provider, fn func(key service.UpstreamKey) (*http.Response, time.Time, error)) (*http.Response, time.Time, error) {
	keys, err := e.providerService.ListActiveKeys(provider)
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(keys) == 0 {
		return nil, time.Time{}, fmt.Errorf("failed to decrypt provider key")
	}
	startIdx := e.providerService.NextStartIndex(provider.ID, len(keys))
	var lastResp *http.Response
	var lastStart time.Time
	var lastErr error
	for i := 0; i < len(keys); i++ {
		key := keys[(startIdx+i)%len(keys)]
		resp, start, err := fn(key)
		if lastResp != nil && lastResp != resp {
			io.Copy(io.Discard, lastResp.Body)
			lastResp.Body.Close()
		}
		lastResp, lastStart, lastErr = resp, start, err
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if !failoverStatus(err, status) {
			return resp, start, err
		}
		if status == 401 || status == 403 {
			_ = e.providerService.DeactivateKey(key.ID)
		}
	}
	return lastResp, lastStart, lastErr
}
```

Need `"io"` and `"omnirelay/internal/service"` in `upstream.go`.

Rewrite `executeUpstream` so it does **not** decrypt once or write on the first network error:

```go
func (rc *requestContext) executeUpstream(adaptedBody map[string]interface{}, endpoint string, isStream bool) (resp *http.Response, startTime time.Time, wroteError bool) {
	provider := rc.provider
	e := rc.engine
	if provider.ProviderType == "gemini" && isStream {
		endpoint = applyGeminiStreamingURL("gemini", endpoint, true)
	}
	adaptedJSON, _ := json.Marshal(adaptedBody)
	modelURL := joinUpstreamURL(provider.APiBaseURL, endpoint)

	resp, startTime, err := e.tryKeys(provider, func(key service.UpstreamKey) (*http.Response, time.Time, error) {
		req, err := http.NewRequest("POST", modelURL, bytes.NewReader(adaptedJSON))
		if err != nil {
			return nil, time.Time{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		copyForwardableRequestHeaders(rc.c, req)
		setProviderAuthHeaders(req, provider.ProviderType, key.Plaintext)
		client := &http.Client{Timeout: 5 * time.Minute}
		start := time.Now()
		resp, err := client.Do(req)
		return resp, start, err
	})
	if err != nil && resp == nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID: &rc.apiKeyID, ProviderID: &provider.ID, Model: rc.fullModelID,
			IsError: true, ErrorMessage: err.Error(), UserID: &rc.userID,
		})
		rc.c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("the model request failed: %v", err)})
		return nil, startTime, true
	}
	return resp, startTime, false
}
```

`buildAndSendChatRequest` already handles `!isSuccessStatus` after `executeUpstream` returns. After failover, a 503 terminal response still goes through that path once — good. Do not log inside `tryKeys`.

`proxyJSONRequest`: take `provider *models.Provider`, drop `providerType, apiKey`. Inside, `tryKeys` + `buildUpstreamRequest(..., key.Plaintext)` + `doUpstream`. Only on terminal failure log + `writeUpstreamErrorBody`. On network terminal error, existing AbortBadGateway.

`executeMessages`: drop `apiKey` arg; call `proxyJSONRequest(c, u, provider, modelURL, adaptedBody, true)`.

`resolveDispatch`: delete the decrypt block; return `""` for apiKey. Do not change the function signature (responses.go already discards it).

`HandlePathRouted` messages branch: remove decrypt; `executeMessages(...)` without apiKey.

`handlePathRoutedProxy`: remove single decrypt. After building `modelURL` / `reqBodyBytes`, wrap `buildUpstreamRequest`+`doUpstream` in `tryKeys`. Write errors only after the loop. Stream copy only if the chosen resp is success.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/proxy/ -v`
Expected: PASS (existing interruption/usage/acl tests use fallback or copied v14 key from `api_key_encrypted` — they must keep working)

Also: `go test ./...` from `backend/`
Expected: PASS

- [ ] **Step 5: Commit** (skip unless asked)

```bash
git add backend/internal/proxy/upstream.go backend/internal/proxy/proxy.go backend/internal/proxy/proxy_helpers.go backend/internal/proxy/chat_handler.go backend/internal/proxy/key_failover_test.go
git commit -m "feat: round-robin and failover across provider API keys"
```

---

### Task 5: Frontend key list on provider dialog

**Files:**
- Modify: `frontend/src/stores/providers.ts`
- Modify: `frontend/src/views/ProvidersView.vue`
- Modify: `frontend/src/locales/en.ts`, `ja.ts`, `ko.ts`

**Interfaces:**
- Consumes: GET providers `api_keys`; POST `/providers/:id/keys`; PATCH `/providers/:id/keys/:kid`; DELETE `/providers/:id/keys/:kid`
- Produces: edit dialog lists prefix + active switch + delete; add-key field; create dialog unchanged (single `api_key`)

- [ ] **Step 1: Types + store methods**

In `providers.ts`:

```ts
interface ProviderAPIKey {
  id: number;
  key_prefix: string;
  is_active: boolean;
  created_at: string;
}

interface Provider {
  // existing fields...
  api_keys?: ProviderAPIKey[];
}
```

```ts
async function addKey(providerId: number, api_key: string) {
  await api.post(`/providers/${providerId}/keys`, { api_key });
  await fetch();
}
async function setKeyActive(providerId: number, keyId: number, is_active: boolean) {
  await api.patch(`/providers/${providerId}/keys/${keyId}`, { is_active });
  await fetch();
}
async function removeKey(providerId: number, keyId: number) {
  await api.delete(`/providers/${providerId}/keys/${keyId}`);
  await fetch();
}
```

Export them from the store return.

On edit save: do **not** send a leftover `api_key` that would insert a duplicate. Strip `api_key` from the PUT payload when editing; use `addKey` for the extra field.

- [ ] **Step 2: Dialog UI**

Create dialog: keep the existing password `api_key` field.

Edit dialog (`editing` truthy, `form.provider_type !== 'custom'`): replace that single field with:

- list `editing.api_keys` (prefix, active checkbox/switch, delete button)
- input `newKey` + button calling `store.addKey(editing.id, newKey)` then `store.fetch()` and refresh `editing` from `store.providers`

Active toggle: `store.setKeyActive(editing.id, key.id, value)`.
Delete: `store.removeKey(...)`.
Show `store.error` / `dialogError` on last-active 400.

Locales — add next to `leaveEmpty` in en/ja/ko:

en:

```
apiKeys: "API Keys",
addKey: "Add key",
keyPrefix: "Prefix",
cannotDeleteLastKey: "At least one active key is required",
```

ja:

```
apiKeys: "APIキー",
addKey: "キーを追加",
keyPrefix: "プレフィックス",
cannotDeleteLastKey: "有効なキーを1つ以上残してください",
```

ko:

```
apiKeys: "API 키",
addKey: "키 추가",
keyPrefix: "접두사",
cannotDeleteLastKey: "활성 키를 하나 이상 남겨 주세요",
```

- [ ] **Step 3: Typecheck**

Run: `bun run build`
Workdir: `frontend/`
Expected: `vue-tsc --noEmit` + vite build succeed

- [ ] **Step 4: Commit** (skip unless asked)

```bash
git add frontend/src/stores/providers.ts frontend/src/views/ProvidersView.vue frontend/src/locales/en.ts frontend/src/locales/ja.ts frontend/src/locales/ko.ts
git commit -m "feat: manage provider API keys in the providers dialog"
```

---

## Self-review

- Spec coverage: v14 copy, fallback, RR memory, failover statuses, 401/403 deactivate, last-active 400, PUT inserts, Test/Sync first key, CORS PATCH, usage terminal-only, frontend list — each has a task.
- `resolveDispatch` decrypt removal is Task 4 so existing tests keep compiling after Task 2.
- `seedKeyFailoverRouter` must use empty `api_key_encrypted` + `AddKey` so v14 copy does not add a ghost key.
- No placeholders. Skip git commit steps unless the user asks.
