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

type ProviderService struct {
	db *sql.DB
	cfg *config.Config
}

func NewProviderService(db *sql.DB, cfg *config.Config) *ProviderService {
	return &ProviderService{db: db, cfg: cfg}
}

func (s *ProviderService) List(userID int64) ([]models.Provider, error) {
	rows, err := s.db.Query(
		"SELECT id, provider_key, name, api_base_url, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE (user_id = ? OR user_id IS NULL) ORDER BY created_at",
		userID,
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
	return providers, nil
}

func (s *ProviderService) GetByKey(providerKey string, userID int64) (*models.Provider, error) {
	var p models.Provider
	err := s.db.QueryRow(
		"SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE provider_key = ? AND is_active = 1 AND (user_id = ? OR user_id IS NULL)",
		providerKey, userID,
	).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProviderService) GetByID(id int64, userID int64) (*models.Provider, error) {
	var p models.Provider
	err := s.db.QueryRow(
		"SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, show_in_model_list, COALESCE(user_id, 0), created_at, updated_at FROM providers WHERE id = ? AND (user_id = ? OR user_id IS NULL)",
		id, userID,
	).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.ShowInModelList, &p.UserID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
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
		if err := s.importSourceModels(provider, req.SourceModels, userID); err != nil {
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

type openaiModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (s *ProviderService) FetchModelsFromProvider(provider *models.Provider) ([]string, error) {
	if provider.ProviderType == "custom" {
		return nil, nil
	}

	apiKey, err := s.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		return nil, err
	}

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
