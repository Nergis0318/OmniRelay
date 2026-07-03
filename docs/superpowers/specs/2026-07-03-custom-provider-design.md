# Custom Provider Feature Design

## Overview

Add a "custom" provider type that lets users aggregate models from multiple existing providers under a single provider key. When a request hits `custom-pro1/somemodel1`, the proxy transparently routes to the real upstream provider (e.g., `other-pro1`) using that provider's adapter, API key, and base URL.

## Database Changes

### Migration v8 — `models.source_provider_key`

```sql
ALTER TABLE models ADD COLUMN source_provider_key TEXT NOT NULL DEFAULT ''
```

Idempotent (checks column exists first). Stores the real upstream provider key for custom-routed models. When empty, the model routes to its owning provider normally.

### Migration v9 — `providers.show_in_model_list`

```sql
ALTER TABLE providers ADD COLUMN show_in_model_list BOOLEAN DEFAULT 1
```

Idempotent. Controls whether this provider's models appear in the public `/v1/models` list.

### Provider Type

Add `'custom'` to the allowed `provider_type` values. Since SQLite doesn't support `ALTER TABLE ... DROP CONSTRAINT`, the DB-level CHECK constraint remains as-is. Application-level validation in `CreateProviderRequest` and `UpdateProviderRequest` is updated to accept `'custom'`.

## Data Model Changes

### `models.Provider`

New field:
```go
ShowInModelList bool `json:"show_in_model_list"`
```

### `models.CreateProviderRequest`

- `APIKey` and `APiBaseURL` become optional when `ProviderType == "custom"`
- New field: `SourceModels []string` — list of `provider_key/model_id` strings to import
- New field: `ShowInModelList *bool` — whether models appear in public list

### `models.UpdateProviderRequest`

- New field: `ShowInModelList *bool`
- New field: `SourceModels []string` — replaces the model list entirely

## Routing Logic

### `resolveDispatch()` (proxy.go)

After resolving `dbModel` via `FindByFullID`:
1. If `dbModel.SourceProviderKey != ""`, look up the real provider by `source_provider_key` instead of `dbModel.ProviderID`
2. Use the real provider's `adapter`, `api_key`, `api_base_url`
3. The `model_id` sent upstream is `dbModel.ModelID` (already stored without prefix)

### `HandlePathRouted` (proxy.go)

Same resolution logic — when the path-routed provider is "custom", the model lookup finds `source_provider_key` and routes to the real upstream.

### `HandleListModels` (proxy.go)

JOIN condition adds `AND p.show_in_model_list = 1` to filter out providers where this is disabled.

## New Endpoint: `GET /admin/models/source-list`

Returns all existing models grouped by provider for the multi-select UI:

**Handler**: `ListSourceModels(svc *service.ModelService) gin.HandlerFunc`

**Response**:
```json
{
  "providers": [
    {
      "provider_key": "other-pro1",
      "provider_type": "openai",
      "name": "Other Provider 1",
      "models": [
        {"model_id": "somemodel1", "display_name": "somemodel1"},
        {"model_id": "gpt-4", "display_name": "GPT-4"}
      ]
    }
  ]
}
```

## Service Layer

### `ProviderService`

- `Create()`: When `provider_type == "custom"`, skip API key encryption. After creating the provider, iterate `SourceModels`, split each on first `/`, look up the source model by `FindByFullID`, and create a new model record under the custom provider with:
  - `provider_id` = custom provider's ID
  - `model_id` = original model_id (without prefix)
  - `provider_key` = custom provider's key
  - `source_provider_key` = source provider's key
  - Pricing fields copied from source model
  - `is_manual = 0` (managed by the custom provider)

- `Update()`: When `SourceModels` is provided, delete existing custom models and re-create from the new list.

- `FetchModelsFromProvider()`: Skip for custom providers (no upstream to fetch from).

### `ModelService`

- `List()`: Add JOIN condition for `show_in_model_list`.
- New method: `ListSourceModels(userID int64) ([]SourceModelGroup, error)` — returns models grouped by provider, excluding custom providers themselves.

## Frontend Changes

### `providers.ts` store

- Add `sourceModels` ref
- Add `fetchSourceModels()` — calls `GET /admin/models/source-list`
- Add `showInModelList` to form

### `ProvidersView.vue`

- When `form.provider_type === "custom"`:
  - Hide `api_base_url` and `api_key` fields
  - Show multi-select: expandable groups per provider, each showing its models with checkboxes
  - Add "Show in public model list" toggle (default: on)
- When editing a custom provider, load current selections

### i18n

Add new translation keys:
- `providers.sourceModels` — "Source Models"
- `providers.selectModelsHint` — "Select models from your existing providers"
- `providers.showInModelList` — "Show in public model list"
- `providers.showInModelListHint` — "When enabled, these models appear in /v1/models"

## Security Considerations

- Custom providers don't store API keys (they delegate to real providers)
- Source model lookup is scoped to the user's ID (same as regular model listing)
- Users can only reference models from providers they own or that are shared (user_id IS NULL)

## Migration Path

Existing databases get `source_provider_key = ''` (default) and `show_in_model_list = 1` (default). No behavior change for existing providers.
