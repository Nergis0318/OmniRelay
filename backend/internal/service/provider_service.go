package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"omnirelay/internal/config"
	"omnirelay/internal/crypto"
	"omnirelay/internal/models"
	"strings"
	"sync"
	"time"
)

// ProviderError carries an HTTP status code for provider-related errors.
type ProviderError struct {
	Message    string
	StatusCode int
}

func (e *ProviderError) Error() string {
	return e.Message
}

var endpointTypes = map[string]bool{
	"openai": true, "anthropic": true, "lmstudio": true, "ollama": true, "gemini": true,
}

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

type ProviderService struct {
	db   *sql.DB
	cfg  *config.Config
	rrMu sync.Mutex
	rr   map[int64]uint64
}

func NewProviderService(db *sql.DB, cfg *config.Config) *ProviderService {
	return &ProviderService{db: db, cfg: cfg, rr: make(map[int64]uint64)}
}

func (s *ProviderService) List(userID int64) ([]models.Provider, error) {
	rows, err := s.db.Query(
		"SELECT id, provider_key, name, api_base_url, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers ORDER BY created_at",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.Provider
	for rows.Next() {
		var p models.Provider
		if err := rows.Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	for i := range providers {
		if providers[i].ProviderType == "custom" {
			s.loadSourceModels(&providers[i])
		}
		if err := s.loadEndpoints(&providers[i]); err != nil {
			return nil, err
		}
		if err := s.loadAPIKeys(&providers[i]); err != nil {
			return nil, err
		}
	}
	return providers, nil
}

func (s *ProviderService) GetByKey(providerKey string, userID int64) (*models.Provider, error) {
	var p models.Provider
	err := s.db.QueryRow(
		"SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE provider_key = ? AND is_active = 1",
		providerKey,
	).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := s.loadEndpoints(&p); err != nil {
		return nil, err
	}
	if err := s.loadAPIKeys(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProviderService) GetByID(id int64, userID int64) (*models.Provider, error) {
	var p models.Provider
	err := s.db.QueryRow(
		"SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE id = ?",
		id,
	).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if p.ProviderType == "custom" {
		s.loadSourceModels(&p)
	}
	if err := s.loadEndpoints(&p); err != nil {
		return nil, err
	}
	if err := s.loadAPIKeys(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProviderService) loadSourceModels(p *models.Provider) error {
	if p.ProviderType != "custom" {
		return nil
	}
	rows, err := s.db.Query(
		"SELECT source_provider_key || '/' || model_id FROM models WHERE provider_id = ? AND source_provider_key IS NOT NULL AND source_provider_key != ''",
		p.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var fullID string
		if err := rows.Scan(&fullID); err != nil {
			return err
		}
		p.SourceModels = append(p.SourceModels, fullID)
	}
	return rows.Err()
}

func (s *ProviderService) Create(req models.CreateProviderRequest, userID int64) (*models.Provider, error) {

	var encrypted string
	if req.ProviderType == "custom" {
		encrypted = ""
	} else {
		if req.APIKey == "" || req.APiBaseURL == "" {
			return nil, &ProviderError{Message: "api_key and api_base_url are required for non-custom providers", StatusCode: 400}
		}
		var err error
		encrypted, err = crypto.Encrypt(req.APIKey, s.cfg.EncryptKey)
		if err != nil {
			return nil, &ProviderError{Message: fmt.Sprintf("failed to encrypt API key: %s", err), StatusCode: 500}
		}
	}

	showInModelList := true
	if req.ShowInModelList != nil {
		showInModelList = *req.ShowInModelList
	}

	result, err := s.db.Exec(
		"INSERT INTO providers (provider_key, name, api_base_url, api_key_encrypted, provider_type, show_in_model_list, user_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
		req.ProviderKey, req.Name, req.APiBaseURL, encrypted, req.ProviderType, showInModelList, userID,
	)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE constraint failed") {
			return nil, &ProviderError{Message: "provider_key already exists", StatusCode: 409}
		}
		if strings.Contains(errStr, "CHECK constraint failed") {
			return nil, &ProviderError{Message: fmt.Sprintf("invalid provider_type: %s", req.ProviderType), StatusCode: 400}
		}
		return nil, &ProviderError{Message: fmt.Sprintf("failed to create provider: %s", err), StatusCode: 500}
	}

	id, _ := result.LastInsertId()
	if encrypted != "" {
		if err := s.insertKey(id, req.APIKey); err != nil {
			return nil, err
		}
	}
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

	if req.ProviderType == "custom" && len(req.SourceModels) > 0 {
		if err := s.importSourceModels(provider, req.SourceModels, userID); err != nil {
			return nil, err
		}
	}

	return provider, nil
}

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
		encrypted, err := crypto.Encrypt(*req.APIKey, s.cfg.EncryptKey)
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
		if err := s.insertKey(id, *req.APIKey); err != nil {
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

	if providerType == "custom" && req.SourceModels != nil {
		_, err = s.db.Exec("DELETE FROM models WHERE provider_id = ? AND user_id = ?", id, userID)
		if err != nil {
			return nil, err
		}
		provider, _ := s.GetByID(id, userID)
		if err := s.importSourceModels(provider, *req.SourceModels, userID); err != nil {
			return nil, err
		}
	}

	if req.Endpoints != nil {
		if providerType == "custom" && len(*req.Endpoints) > 0 {
			return nil, &ProviderError{Message: "endpoints are not supported for custom providers", StatusCode: 400}
		}
		if err := s.saveEndpoints(id, *req.Endpoints); err != nil {
			return nil, err
		}
	}

	return s.GetByID(id, userID)
}

func (s *ProviderService) Delete(id int64, userID int64) error {
	_, err := s.db.Exec("DELETE FROM models WHERE provider_id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("DELETE FROM providers WHERE id = ? AND user_id = ?", id, userID)
	return err
}

func (s *ProviderService) DecryptAPIKey(encrypted string) (string, error) {
	return crypto.Decrypt(encrypted, s.cfg.EncryptKey)
}

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
	if len(keys) == 0 {
		var n int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM provider_api_keys WHERE provider_id = ?`,
			provider.ID,
		).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 && provider.APIKeyEncrypted != "" {
			plain, err := s.DecryptAPIKey(provider.APIKeyEncrypted)
			if err != nil {
				return nil, err
			}
			return []UpstreamKey{{ID: 0, Plaintext: plain}}, nil
		}
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

func (s *ProviderService) importSourceModels(customProvider *models.Provider, sourceModels []string, userID int64) error {
	for _, fullID := range sourceModels {
		parts := strings.SplitN(fullID, "/", 2)
		if len(parts) != 2 {
			continue
		}
		sourceProviderKey := parts[0]
		sourceModelID := parts[1]

		var sourceModel models.Model
		err := s.db.QueryRow(
			`SELECT m.id, m.provider_id, m.model_id, COALESCE(m.display_name,''), m.provider_key,
				m.is_manual, m.source_provider_key, m.input_price_per_1mtok, m.output_price_per_1mtok, m.cache_write_5m_price_per_1mtok, m.cache_write_1h_price_per_1mtok, m.cache_read_price_per_1mtok, m.context_window, COALESCE(m.user_id, 0), m.created_at
			FROM models m JOIN providers p ON m.provider_id = p.id
			WHERE m.provider_key = ? AND m.model_id = ? AND p.is_active = 1 AND (m.user_id = ? OR m.user_id IS NULL)`,
			sourceProviderKey, sourceModelID, userID,
		).Scan(&sourceModel.ID, &sourceModel.ProviderID, &sourceModel.ModelID, &sourceModel.DisplayName, &sourceModel.ProviderKey,
			&sourceModel.IsManual, &sourceModel.SourceProviderKey, &sourceModel.InputPricePer1MTok, &sourceModel.OutputPricePer1MTok, &sourceModel.CacheWrite5mPricePer1MTok, &sourceModel.CacheWrite1hPricePer1MTok, &sourceModel.CacheReadPricePer1MTok, &sourceModel.ContextWindow, &sourceModel.UserID, &sourceModel.CreatedAt)
		if err != nil {
			continue
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

func (s *ProviderService) FirstModelID(providerID int64, userID int64) (string, error) {
	var modelID string
	err := s.db.QueryRow(
		"SELECT model_id FROM models WHERE provider_id = ? AND (user_id = ? OR user_id IS NULL) ORDER BY created_at LIMIT 1",
		providerID, userID,
	).Scan(&modelID)
	if err != nil {
		return "", err
	}
	return modelID, nil
}

type openaiModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (s *ProviderService) FetchModelsFromProvider(provider *models.Provider) ([]string, error) {
	if provider.ProviderType == "custom" {
		return nil, nil
	}

	uk, err := s.FirstActiveKey(provider)
	if err != nil {
		return nil, err
	}
	apiKey := uk.Plaintext

	baseURL := strings.TrimRight(provider.APiBaseURL, "/")
	var url string
	switch provider.ProviderType {
	case "anthropic":
		if strings.HasSuffix(baseURL, "/v1") {
			url = baseURL + "/models?limit=1000"
		} else {
			url = baseURL + "/v1/models?limit=1000"
		}
	case "gemini":
		url = baseURL + "/models"
	case "ollama":
		url = strings.TrimSuffix(baseURL, "/v1") + "/api/tags"
	default:
		url = baseURL + "/models"
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch provider.ProviderType {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		req.Header.Set("x-goog-api-key", apiKey)
	case "ollama":
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var modelIDs []string
	switch provider.ProviderType {
	case "gemini":
		var result struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		for _, m := range result.Models {
			name := m.Name
			if idx := strings.LastIndex(name, "/"); idx >= 0 {
				name = name[idx+1:]
			}
			if name != "" {
				modelIDs = append(modelIDs, name)
			}
		}
	case "ollama":
		var result struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		for _, m := range result.Models {
			if m.Name != "" {
				modelIDs = append(modelIDs, m.Name)
			}
		}
	default:
		var listResp openaiModelListResponse
		if err := json.Unmarshal(body, &listResp); err != nil {
			return nil, err
		}
		for _, m := range listResp.Data {
			if m.ID != "" {
				modelIDs = append(modelIDs, m.ID)
			}
		}
	}

	return modelIDs, nil
}
