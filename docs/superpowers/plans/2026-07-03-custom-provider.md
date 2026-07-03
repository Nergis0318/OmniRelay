# Custom Provider Feature Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "custom" provider type that lets users aggregate models from multiple existing providers under a single provider key, with transparent proxy routing to the real upstream provider.

**Architecture:** New `provider_type = "custom"` with no upstream API key/URL. A `source_provider_key` column on models enables transparent routing to the real provider. A `show_in_model_list` column on providers controls public model list visibility. The proxy's `resolveDispatch` checks `source_provider_key` and overrides the provider/adapter/apiKey lookup.

**Tech Stack:** Go (Gin), SQLite (modernc.org), Vue 3, Vuetify 3, Pinia

## Global Constraints

- Go: `gofmt` tabs
- Frontend: two-space indentation, views as `PascalCaseView.vue`, stores as lowercase files
- Use `go vet ./...` before claiming a fix works
- All migrations must be idempotent (check column exists with `PRAGMA table_info`)
- Bun is the frontend package manager (not npm)
- Models are linked to providers via `provider_id`; full model ID is `provider_key/model_id`

---

## Task 1: Database Migration — `source_provider_key` and `show_in_model_list`

**Files:**
- Modify: `backend/internal/database/migrations.go`

**Step 1: Add migration v8 and v9**

Append two new entries to the `migrations` slice in `migrations.go` after v7:

```go
{
    version: 8,
    up: func(tx *sql.Tx) error {
        hasColumn, err := hasColumn(tx, "models", "source_provider_key")
        if err != nil {
            return err
        }
        if !hasColumn {
            if _, err := tx.Exec(`ALTER TABLE models ADD COLUMN source_provider_key TEXT NOT NULL DEFAULT ''`); err != nil {
                return err
            }
        }
        return nil
    },
},
{
    version: 9,
    up: func(tx *sql.Tx) error {
        hasColumn, err := hasColumn(tx, "providers", "show_in_model_list")
        if err != nil {
            return err
        }
        if !hasColumn {
            if _, err := tx.Exec(`ALTER TABLE providers ADD COLUMN show_in_model_list BOOLEAN DEFAULT 1`); err != nil {
                return err
            }
        }
        return nil
    },
},
```

**Step 2: Run migrations on existing test**

```bash
cd backend && go test ./internal/database/... -v
```

Expected: PASS

**Step 3: Commit**

```bash
git add backend/internal/database/migrations.go
git commit -m "feat: add source_provider_key and show_in_model_list migrations"
```

---

## Task 2: Update Model Structs

**Files:**
- Modify: `backend/internal/models/model.go`
- Modify: `backend/internal/models/provider.go`

**Step 1: Add `SourceProviderKey` to Model struct**

In `backend/internal/models/model.go`, add after `IsManual`:

```go
IsManual                  bool      `json:"is_manual"`
SourceProviderKey         string    `json:"source_provider_key"`
```

**Step 2: Add `ShowInModelList` to Provider struct**

In `backend/internal/models/provider.go`, add after `IsActive`:

```go
IsActive        bool      `json:"is_active"`
ShowInModelList bool      `json:"show_in_model_list"`
```

**Step 3: Update `CreateProviderRequest`**

Add after `ProviderType`:

```go
ProviderType     string   `json:"provider_type" binding:"required,oneof=openai anthropic lmstudio ollama gemini custom"`
SourceModels     []string `json:"source_models"`
ShowInModelList  *bool    `json:"show_in_model_list"`
```

Change `APIKey` and `APiBaseURL` to remove `binding:"required"` (make them optional now):

```go
APIKey     string `json:"api_key"`
APiBaseURL string `json:"api_base_url"`
```

**Step 4: Update `UpdateProviderRequest`**

Add after `IsActive`:

```go
IsActive         *bool    `json:"is_active"`
SourceModels     []string `json:"source_models"`
ShowInModelList  *bool    `json:"show_in_model_list"`
```

Change `ProviderType` binding to include custom:

```go
ProviderType *string `json:"provider_type" binding:"omitempty,oneof=openai anthropic lmstudio ollama gemini custom"`
```

**Step 5: Run go vet**

```bash
cd backend && go vet ./...
```

Expected: No errors

**Step 6: Commit**

```bash
git add backend/internal/models/model.go backend/internal/models/provider.go
git commit -m "feat: add source_provider_key and show_in_model_list to models"
```

---

## Task 3: Update Service Layer — Model Query and Source Models

**Files:**
- Modify: `backend/internal/service/model_service.go`

**Step 1: Update `Model.List()` to filter by `show_in_model_list`**

In `model_service.go`, modify the query in `List()` to add the filter:

```go
func (s *ModelService) List(providerKey string, userID int64) ([]models.Model, error) {
    query := `SELECT m.id, m.provider_id, m.model_id, COALESCE(m.display_name,''), m.provider_key,
        m.is_manual, m.source_provider_key, m.input_price_per_1mtok, m.output_price_per_1mtok, m.cache_write_5m_price_per_1mtok, m.cache_write_1h_price_per_1mtok, m.cache_read_price_per_1mtok, m.context_window, COALESCE(m.user_id, 0), m.created_at
        FROM models m JOIN providers p ON m.provider_id = p.id WHERE p.is_active = 1 AND p.show_in_model_list = 1 AND (m.user_id = ? OR m.user_id IS NULL)`
    args := []interface{}{userID}

    if providerKey != "" {
        query += " AND m.provider_key = ?"
        args = append(args, providerKey)
    }

    query += " ORDER BY m.provider_key, m.model_id"

    rows, err := s.db.Query(query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var result []models.Model
    for rows.Next() {
        var m models.Model
        if err := rows.Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.ProviderKey,
            &m.IsManual, &m.SourceProviderKey, &m.InputPricePer1MTok, &m.OutputPricePer1MTok, &m.CacheWrite5mPricePer1MTok, &m.CacheWrite1hPricePer1MTok, &m.CacheReadPricePer1MTok, &m.ContextWindow, &m.UserID, &m.CreatedAt); err != nil {
            return nil, err
        }
        result = append(result, m)
    }
    return result, nil
}
```

**Step 2: Update `SyncFromProvider` scan row**

Update the `rows.Scan` call to include `source_provider_key`:

```go
_, err = tx.Query(
    "SELECT model_id, source_provider_key, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok FROM models WHERE provider_id = ? AND is_manual = 0 AND user_id = ?",
    providerID, userID,
)
if err != nil {
    return err
}
for rows.Next() {
    var modelID string
    var sp savedPrice
    var sourceProviderKey string
    if err2 := rows.Scan(&modelID, &sourceProviderKey, &sp.InputPrice, &sp.OutputPrice, &sp.CacheWrite5mPrice, &sp.CacheWrite1hPrice, &sp.CacheReadPrice); err2 != nil {
        rows.Close()
        return err2
    }
    existingPrices[modelID] = sp
    existingSourceKeys[modelID] = sourceProviderKey
}
rows.Close()
```

Wait — the `savedPrice` struct and map need adjustment. Let me redo this properly.

Actually, since source_provider_key is only meaningful for custom providers, and `SyncFromProvider` is for syncing from a real upstream (not custom), we don't need to preserve it during sync. Let me remove that change from `SyncFromProvider` — the `INSERT OR IGNORE` will set it to '' (default) which is correct for synced models.

But the scan needs to include the column... actually, let me re-add the column to the SELECT since the table has it. Let me re-check: the SELECT currently reads 7 columns. If we add `source_provider_key` as a column but don't include it in SELECT, that's fine. We don't need to scan it.

Actually wait, the issue is simpler. The `SyncFromProvider` SELECT doesn't currently include `source_provider_key`. We just need to add it to the query and scan:

```go
rows, err := tx.Query(
    "SELECT model_id, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok FROM models WHERE provider_id = ? AND is_manual = 0 AND user_id = ?",
    providerID, userID,
)
```

This query is fine as-is because `source_provider_key` defaults to ''. When we delete all non-manual models and re-insert them, they'll all get `source_provider_key = ''` which is correct. No change needed to `SyncFromProvider`.

**Step 3: Update `Create` scan row**

```go
_, err = s.db.Exec(
    "INSERT INTO models (provider_id, model_id, display_name, provider_key, is_manual, source_provider_key, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok, context_window, user_id) VALUES (?, ?, ?, ?, 1, '', ?, ?, ?, ?, ?, ?, ?)",
    req.ProviderID, req.ModelID, displayName, providerKey, req.InputPricePer1MTok, req.OutputPricePer1MTok, req.CacheWrite5mPricePer1MTok, req.CacheWrite1hPricePer1MTok, req.CacheReadPricePer1MTok, req.ContextWindow, userID,
)
```

**Step 4: Update `GetByID` scan row**

Add `&m.SourceProviderKey` to the Scan call, and update the query to include `source_provider_key`:

```go
err := s.db.QueryRow(
    `SELECT id, provider_id, model_id, COALESCE(display_name,''), provider_key,
        is_manual, source_provider_key, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok, context_window, COALESCE(user_id, 0), created_at FROM models WHERE id = ? AND (user_id = ? OR user_id IS NULL)`,
    id, userID,
).Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.ProviderKey,
    &m.IsManual, &m.SourceProviderKey, &m.InputPricePer1MTok, &m.OutputPricePer1MTok, &m.CacheWrite5mPricePer1MTok, &m.CacheWrite1hPricePer1MTok, &m.CacheReadPricePer1MTok, &m.ContextWindow, &m.UserID, &m.CreatedAt)
```

**Step 5: Add `ListSourceModels` method**

Add new types and method to `model_service.go`:

```go
type SourceModelInfo struct {
    ModelID     string `json:"model_id"`
    DisplayName string `json:"display_name"`
}

type SourceModelGroup struct {
    ProviderKey  string           `json:"provider_key"`
    ProviderType string           `json:"provider_type"`
    Name         string           `json:"name"`
    Models       []SourceModelInfo `json:"models"`
}

func (s *ModelService) ListSourceModels(userID int64) ([]SourceModelGroup, error) {
    rows, err := s.db.Query(`
        SELECT p.provider_key, p.provider_type, p.name, m.model_id, COALESCE(m.display_name, '')
        FROM models m
        JOIN providers p ON m.provider_id = p.id
        WHERE p.is_active = 1 AND p.provider_type <> 'custom' AND (m.user_id = ? OR m.user_id IS NULL)
        ORDER BY p.provider_key, m.model_id
    `, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    groupMap := make(map[string]*SourceModelGroup)
    var order []string

    for rows.Next() {
        var pk, ptype, name, mid, dname string
        if err := rows.Scan(&pk, &ptype, &name, &mid, &dname); err != nil {
            return nil, err
        }
        group, ok := groupMap[pk]
        if !ok {
            group = &SourceModelGroup{
                ProviderKey:  pk,
                ProviderType: ptype,
                Name:         name,
            }
            groupMap[pk] = group
            order = append(order, pk)
        }
        group.Models = append(group.Models, SourceModelInfo{
            ModelID:     mid,
            DisplayName: dname,
        })
    }

    var result []SourceModelGroup
    for _, pk := range order {
        result = append(result, *groupMap[pk])
    }
    return result, nil
}
```

**Step 6: Run tests**

```bash
cd backend && go test ./internal/service/... -v
```

Expected: PASS

**Step 7: Commit**

```bash
git add backend/internal/service/model_service.go
git commit -m "feat: add source_provider_key to model queries, ListSourceModels endpoint"
```

---

## Task 4: Update Provider Service for Custom Type

**Files:**
- Modify: `backend/internal/service/provider_service.go`

**Step 1: Update `List()` scan row**

Add `&p.ShowInModelList` to the Scan:

```go
err := rows.Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt)
```

Update the query to include `show_in_model_list`:

```sql
SELECT id, provider_key, name, api_base_url, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE (user_id = ? OR user_id IS NULL) ORDER BY created_at
```

**Step 2: Update `GetByKey()` scan row**

Update query and scan:

```go
err := s.db.QueryRow(
    "SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE provider_key = ? AND is_active = 1 AND (user_id = ? OR user_id IS NULL)",
    providerKey, userID,
).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt)
```

**Step 3: Update `GetByID()` scan row**

Same pattern:

```go
err := s.db.QueryRow(
    "SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE id = ? AND (user_id = ? OR user_id IS NULL)",
    id, userID,
).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt)
```

**Step 4: Update `Create()` for custom provider**

```go
func (s *ProviderService) Create(req models.CreateProviderRequest, userID int64) (*models.Provider, error) {
    cfg := config.Load()

    var encrypted string
    if req.ProviderType == "custom" {
        // Custom providers don't need API key or base URL
        encrypted = ""
    } else {
        if req.APIKey == "" || req.APiBaseURL == "" {
            return nil, errors.New("api_key and api_base_url are required for non-custom providers")
        }
        var err error
        encrypted, err = crypto.Encrypt(req.APIKey, cfg.EncryptKey)
        if err != nil {
            return nil, fmt.Errorf("failed to encrypt API key: %w", err)
        }
    }

    showInModelList := true
    if req.ShowInModelList != nil {
        showInModelList = *req.ShowInModelList
    }

    result, err := s.db.Exec(
        "INSERT INTO providers (provider_key, name, api_base_url, api_key_encrypted, provider_type, show_in_model_list, user_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
        req.ProviderKey, req.Name, "", encrypted, req.ProviderType, showInModelList, userID,
    )
    if err != nil {
        return nil, errors.New("provider_key already exists")
    }

    id, _ := result.LastInsertId()
    provider, err := s.GetByID(id, userID)
    if err != nil {
        return nil, err
    }

    // Import source models for custom providers
    if req.ProviderType == "custom" && len(req.SourceModels) > 0 {
        if err := s.importSourceModels(provider, req.SourceModels, userID); err != nil {
            return nil, err
        }
    }

    return provider, nil
}
```

**Step 5: Add `importSourceModels` helper**

```go
func (s *ProviderService) importSourceModels(provider *models.Provider, sourceModels []string, userID int64) error {
    for _, fullID := range sourceModels {
        parts := strings.SplitN(fullID, "/", 2)
        if len(parts) != 2 {
            continue
        }
        sourceProviderKey := parts[0]
        sourceModelID := parts[1]

        // Find the source model to copy pricing
        sourceModel, err := s.modelService.FindByFullID(fullID, userID)
        if err != nil {
            continue // skip models we can't find
        }

        _, err = s.db.Exec(
            "INSERT INTO models (provider_id, model_id, display_name, provider_key, is_manual, source_provider_key, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok, context_window, user_id) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?)",
            provider.ID, sourceModelID, sourceModelID, provider.ProviderKey, sourceProviderKey,
            sourceModel.InputPricePer1MTok, sourceModel.OutputPricePer1MTok,
            sourceModel.CacheWrite5mPricePer1MTok, sourceModel.CacheWrite1hPricePer1MTok,
            sourceModel.CacheReadPricePer1MTok, sourceModel.ContextWindow, userID,
        )
        if err != nil {
            continue
        }
    }
    return nil
}
```

Wait — `provider_service.go` doesn't have access to `modelService`. The ProviderService only has `db`. Let me pass the model service or use the DB directly.

Actually, looking at the code, `ProviderService` only has `db *sql.DB`. We need to look up source model pricing using the DB directly. Let me rewrite:

```go
func (s *ProviderService) importSourceModels(customProvider *models.Provider, sourceModels []string, userID int64) error {
    for _, fullID := range sourceModels {
        parts := strings.SplitN(fullID, "/", 2)
        if len(parts) != 2 {
            continue
        }
        sourceProviderKey := parts[0]
        sourceModelID := parts[1]

        // Find the source model to copy pricing
        var sourceModel models.Model
        err := s.db.QueryRow(
            `SELECT m.id, m.provider_id, m.model_id, COALESCE(m.display_name,''), m.provider_key,
                m.is_manual, source_provider_key, m.input_price_per_1mtok, m.output_price_per_1mtok, m.cache_write_5m_price_per_1mtok, m.cache_write_1h_price_per_1mtok, m.cache_read_price_per_1mtok, m.context_window, COALESCE(m.user_id, 0), m.created_at
            FROM models m JOIN providers p ON m.provider_id = p.id
            WHERE m.provider_key = ? AND m.model_id = ? AND p.is_active = 1 AND (m.user_id = ? OR m.user_id IS NULL)`,
            sourceProviderKey, sourceModelID, userID,
        ).Scan(&sourceModel.ID, &sourceModel.ProviderID, &sourceModel.ModelID, &sourceModel.DisplayName, &sourceModel.ProviderKey,
            &sourceModel.IsManual, &sourceModel.SourceProviderKey, &sourceModel.InputPricePer1MTok, &sourceModel.OutputPricePer1MTok, &sourceModel.CacheWrite5mPricePer1MTok, &sourceModel.CacheWrite1hPricePer1MTok, &sourceModel.CacheReadPricePer1MTok, &sourceModel.ContextWindow, &sourceModel.UserID, &sourceModel.CreatedAt)
        if err != nil {
            continue // skip models we can't find
        }

        _, err = s.db.Exec(
            "INSERT INTO models (provider_id, model_id, display_name, provider_key, is_manual, source_provider_key, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok, context_window, user_id) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?)",
            customProvider.ID, sourceModelID, sourceModelID, customProvider.ProviderKey, sourceProviderKey,
            sourceModel.InputPricePer1MTok, sourceModel.OutputPricePer1MTok,
            sourceModel.CacheWrite5mPricePer1MTok, sourceModel.CacheWrite1hPricePer1MTok,
            sourceModel.CacheReadPricePer1MTok, sourceModel.ContextWindow, userID,
        )
        if err != nil {
            continue
        }
    }
    return nil
}
```

**Step 6: Update `Update()` for custom providers**

Add handling for `ShowInModelList` and `SourceModels`:

```go
func (s *ProviderService) Update(id int64, userID int64, req models.UpdateProviderRequest) (*models.Provider, error) {
    existing, err := s.GetByID(id, userID)
    if err != nil {
        return nil, err
    }

    name := existing.Name
    apiBaseURL := existing.APiBaseURL
    providerType := existing.ProviderType
    isActive := existing.IsActive
    showInModelList := existing.ShowInModelList

    if req.Name != nil {
        name = *req.Name
    }
    if req.APiBaseURL != nil {
        apiBaseURL = *req.APiBaseURL
    }
    if req.ProviderType != nil {
        providerType = *req.ProviderType
    }
    if req.IsActive != nil {
        isActive = *req.IsActive
    }
    if req.ShowInModelList != nil {
        showInModelList = *req.ShowInModelList
    }

    if req.APIKey != nil && *req.APIKey != "" {
        cfg := config.Load()
        encrypted, err := crypto.Encrypt(*req.APIKey, cfg.EncryptKey)
        if err != nil {
            return nil, err
        }
        _, err = s.db.Exec(
            "UPDATE providers SET name=?, api_base_url=?, api_key_encrypted=?, provider_type=?, is_active=?, show_in_model_list=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?",
            name, apiBaseURL, encrypted, providerType, isActive, showInModelList, id, userID,
        )
        if err != nil {
            return nil, err
        }
    } else {
        _, err = s.db.Exec(
            "UPDATE providers SET name=?, api_base_url=?, provider_type=?, is_active=?, show_in_model_list=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?",
            name, apiBaseURL, providerType, isActive, showInModelList, id, userID,
        )
        if err != nil {
            return nil, err
        }
    }

    // Update source models if provided (for custom providers)
    if providerType == "custom" && req.SourceModels != nil {
        // Delete existing custom models
        _, err = s.db.Exec("DELETE FROM models WHERE provider_id = ? AND user_id = ?", id, userID)
        if err != nil {
            return nil, err
        }
        // Import new ones
        provider, _ := s.GetByID(id, userID)
        if err := s.importSourceModels(provider, req.SourceModels, userID); err != nil {
            return nil, err
        }
    }

    return s.GetByID(id, userID)
}
```

**Step 7: Run tests**

```bash
cd backend && go test ./internal/service/... -v
```

Expected: PASS

**Step 8: Commit**

```bash
git add backend/internal/service/provider_service.go
git commit -m "feat: support custom provider type with source model import"
```

---

## Task 5: Add `ListSourceModels` Handler and Route

**Files:**
- Modify: `backend/internal/handlers/models.go`
- Modify: `backend/cmd/server/main.go`

**Step 1: Add `ListSourceModels` handler**

In `backend/internal/handlers/models.go`, add:

```go
func ListSourceModels(svc *service.ModelService) gin.HandlerFunc {
    return func(c *gin.Context) {
        userID := c.GetInt64("user_id")
        groups, err := svc.ListSourceModels(userID)
        if err != nil {
            apiresponse.AbortAdminInternal(c, err.Error())
            return
        }
        if groups == nil {
            groups = []service.SourceModelGroup{}
        }
        c.JSON(http.StatusOK, gin.H{"providers": groups})
    }
}
```

**Step 2: Add route**

In `backend/cmd/server/main.go`, add inside the `adminAuth` group (after `adminAuth.POST("/providers/:id/sync", ...)`):

```go
adminAuth.GET("/models/source-list", handlers.ListSourceModels(modelService))
```

**Step 3: Check compilation**

```bash
cd backend && go build ./...
```

Expected: No errors

**Step 4: Commit**

```bash
git add backend/internal/handlers/models.go backend/cmd/server/main.go
git commit -m "feat: add ListSourceModels handler and route"
```

---

## Task 6: Update Proxy Routing for Custom Providers

**Files:**
- Modify: `backend/internal/proxy/proxy.go`

**Step 1: Update `resolveDispatch` for source provider routing**

Modify `resolveDispatch` to check `source_provider_key`:

```go
func (e *Engine) resolveDispatch(c *gin.Context, fullModelID string, userID int64, errFmt apiresponse.Format) (*models.Model, *models.Provider, Adapter, string, bool) {
    dbModel, err := e.resolveModel(fullModelID, userID)
    if err != nil {
        apiresponse.AbortNotFound(c, errFmt, fmt.Sprintf("The model '%s' does not exist", fullModelID), "model")
        return nil, nil, nil, "", false
    }

    var provider *models.Provider
    if dbModel.SourceProviderKey != "" {
        // Custom provider model — route to the real upstream
        provider, err = e.providerService.GetByKey(dbModel.SourceProviderKey, userID)
    } else {
        provider, err = e.providerService.GetByID(dbModel.ProviderID, userID)
    }
    if err != nil {
        apiresponse.AbortInvalidRequest(c, errFmt, "provider not found or inactive", "model")
        return nil, nil, nil, "", false
    }

    adapter := e.getAdapter(provider.ProviderType)
    if adapter == nil {
        apiresponse.AbortInternal(c, errFmt, "unsupported provider type")
        return nil, nil, nil, "", false
    }

    apiKey, err := e.providerService.DecryptAPIKey(provider.APIKeyEncrypted)
    if err != nil {
        apiresponse.AbortInternal(c, errFmt, "failed to decrypt provider key")
        return nil, nil, nil, "", false
    }

    return dbModel, provider, adapter, apiKey, true
}
```

**Step 2: Update `HandleListModels` for `show_in_model_list`**

This is already done in `ModelService.List()` (Task 3), so no changes needed here. The proxy already calls `e.modelService.List("", userID)`.

**Step 3: Update `HandlePathRouted` for source provider routing**

In `HandlePathRouted`, after resolving `dbModel`, add source provider lookup:

```go
// After: dbModel, _ := e.resolveModel(fullModelID, userID)
// Add: override provider if model has source_provider_key
if dbModel != nil && dbModel.SourceProviderKey != "" {
    sourceProvider, err := e.providerService.GetByKey(dbModel.SourceProviderKey, userID)
    if err == nil {
        provider = sourceProvider
    }
}
```

Place this after `dbModel, _ := e.resolveModel(fullModelID, userID)`:

```go
dbModel, _ := e.resolveModel(fullModelID, userID)
if dbModel != nil && dbModel.SourceProviderKey != "" {
    if sourceProvider, serr := e.providerService.GetByKey(dbModel.SourceProviderKey, userID); serr == nil {
        provider = sourceProvider
    }
}
adapter = e.getAdapter(provider.ProviderType)
```

**Step 4: Run proxy tests**

```bash
cd backend && go test ./internal/proxy/... -v
```

Expected: PASS

**Step 5: Run all tests**

```bash
cd backend && go test ./... -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add backend/internal/proxy/proxy.go
git commit -m "feat: add source_provider_key routing to proxy"
```

---

## Task 7: Update Frontend Store

**Files:**
- Modify: `frontend/src/stores/providers.ts`

**Step 1: Add `SourceModelGroup` interface and state**

```typescript
interface SourceModel {
  model_id: string;
  display_name: string;
}

interface SourceModelGroup {
  provider_key: string;
  provider_type: string;
  name: string;
  models: SourceModel[];
}
```

**Step 2: Add `sourceModels` and `showInModelList` refs and methods**

```typescript
const sourceModels = ref<SourceModelGroup[]>([]);

async function fetchSourceModels() {
  try {
    const { data } = await api.get("/models/source-list");
    sourceModels.value = data.providers;
  } catch (err: any) {
    // Non-critical — UI will just not show the multi-select
  }
}
```

**Step 3: Update `CreateProviderPayload`**

Add:

```typescript
interface CreateProviderPayload {
  provider_key: string;
  name: string;
  api_base_url?: string;
  api_key?: string;
  provider_type: string;
  source_models?: string[];
  show_in_model_list?: boolean;
}
```

**Step 4: Update `create` and `update` signatures**

```typescript
async function create(payload: CreateProviderPayload) {
  // ...same as before but with any payload fields
}
```

**Step 5: Export new state**

```typescript
return { providers, loading, error, sourceModels, fetch, fetchSourceModels, create, update, remove, syncModels, clearError };
```

**Step 6: Commit**

```bash
git add frontend/src/stores/providers.ts
git commit -m "feat: add source models state and fetch to providers store"
```

---

## Task 8: Update Frontend Provider View

**Files:**
- Modify: `frontend/src/views/ProvidersView.vue`

**Step 1: Update form data**

Add `show_in_model_list` and `source_models`:

```typescript
const form = ref({
  provider_key: "",
  name: "",
  api_base_url: "",
  api_key: "",
  provider_type: "openai",
  auto_sync: true,
  show_in_model_list: true,
  source_models: [] as string[],
});
```

**Step 2: Update `providerTypes`**

```typescript
const providerTypes = ["custom", "openai", "anthropic", "lmstudio", "ollama", "gemini"];
```

**Step 3: Update `openDialog` to reset new fields**

In the else branch (new provider):

```typescript
source_models: [],
show_in_model_list: true,
```

For editing:

```typescript
source_models: [],
show_in_model_list: provider.show_in_model_list ?? true,
```

**Step 4: Add UI for custom provider section in template**

After the provider_type select, add:

```vue
<div v-if="form.provider_type === 'custom'" class="field-group">
  <label class="field-label">{{ $t("providers.sourceModels") }}</label>
  <p class="field-hint">{{ $t("providers.selectModelsHint") }}</p>
  <div class="source-models-list">
    <div
      v-for="group in store.sourceModels"
      :key="group.provider_key"
      class="source-model-group"
    >
      <div class="source-model-group-header">
        <span class="source-model-group-name">{{ group.name }}</span>
        <span class="source-model-group-type">{{ group.provider_type }}</span>
      </div>
      <div
        v-for="model in group.models"
        :key="model.model_id"
        class="source-model-item"
      >
        <label class="source-model-label">
          <input
            type="checkbox"
            :value="`${group.provider_key}/${model.model_id}`"
            v-model="form.source_models"
            class="checkbox"
          />
          <span class="source-model-id">{{ model.model_id }}</span>
        </label>
      </div>
    </div>
    <div v-if="!store.sourceModels.length" class="empty-source-models">
      {{ $t("providers.noSourceModels") }}
    </div>
  </div>
</div>

<label class="checkbox-row">
  <input type="checkbox" v-model="form.show_in_model_list" class="checkbox" />
  <div>
    <span class="checkbox-label">{{ $t("providers.showInModelList") }}</span>
    <span class="checkbox-hint">{{
      $t("providers.showInModelListHint")
    }}</span>
  </div>
</label>
```

**Step 5: Hide api_base_url and api_key when type is custom**

```vue
<div v-if="form.provider_type !== 'custom'" class="field-group">
  <!-- api_base_url field -->
</div>
<div v-if="form.provider_type !== 'custom'" class="field-group">
  <!-- api_key field -->
</div>
<div v-if="form.provider_type === 'custom'" class="field-group">
  <!-- label explaining no key needed -->
</div>
```

**Step 6: Add `fetchSourceModels` call on dialog open**

In `openDialog`, before `dialog.value = true`:

```typescript
if (form.value.provider_type === 'custom') {
  store.fetchSourceModels();
}
```

Actually, let's just call it once when the view loads:

```typescript
onMounted(() => {
  checkMobile();
  window.addEventListener("resize", checkMobile);
  store.fetch();
  store.fetchSourceModels();
});
```

**Step 7: Add scoped styles**

```css
.source-models-list {
  max-height: 300px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 8px;
  margin-top: 4px;
}
.source-model-group {
  margin-bottom: 8px;
}
.source-model-group-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
}
.source-model-group-name {
  font-weight: 600;
  font-size: 13px;
}
.source-model-group-type {
  font-size: 11px;
  background: var(--chip-bg);
  padding: 1px 6px;
  border-radius: 4px;
}
.source-model-item {
  padding: 2px 0 2px 12px;
}
.source-model-label {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
.source-model-id {
  font-size: 12px;
  font-family: monospace;
}
.empty-source-models {
  padding: 16px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}
.field-hint {
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 4px;
}
```

**Step 8: Commit**

```bash
git add frontend/src/views/ProvidersView.vue
git commit -m "feat: add custom provider UI with multi-select source models"
```

---

## Task 9: Add i18n Keys

**Files:**
- Modify: `frontend/src/locales/en.ts`
- Modify: `frontend/src/locales/ja.ts`
- Modify: `frontend/src/locales/ko.ts`

**Step 1: Add English keys in `providers` section**

```typescript
sourceModels: "Source Models",
selectModelsHint: "Select models from existing providers to include",
noSourceModels: "No models available from other providers",
showInModelList: "Show in public model list",
showInModelListHint: "When enabled, these models appear in /v1/models",
```

**Step 2: Add Japanese keys**

Same structure in `ja.ts`.

**Step 3: Add Korean keys**

Same structure in `ko.ts`.

**Step 4: Commit**

```bash
git add frontend/src/locales/
git commit -m "feat: add i18n keys for custom provider feature"
```

---

## Task 10: Verification

**Step 1: Run all Go tests**

```bash
cd backend && go test ./... -count=1
```

Expected: ALL PASS

**Step 2: Run go vet**

```bash
cd backend && go vet ./...
```

Expected: No errors

**Step 3: Build frontend**

```bash
cd frontend && bun run build
```

Expected: Build succeeds, type check passes

**Step 4: Verify migration idempotency**

```bash
cd backend && go test ./internal/database/... -v
```

Expected: PASS (migrations can run multiple times without error)

**Step 5: Final commit**

```bash
git status
```

Show the user the complete diff of all changes.
