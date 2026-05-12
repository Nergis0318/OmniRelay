package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"omnirelay/internal/crypto"
	"omnirelay/internal/config"
	"omnirelay/internal/models"
	"time"
)

type ProviderService struct {
	db *sql.DB
}

func NewProviderService(db *sql.DB) *ProviderService {
	return &ProviderService{db: db}
}

func (s *ProviderService) List() ([]models.Provider, error) {
	rows, err := s.db.Query(
		"SELECT id, provider_key, name, api_base_url, provider_type, is_active, created_at, updated_at FROM providers ORDER BY created_at",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.Provider
	for rows.Next() {
		var p models.Provider
		if err := rows.Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.ProviderType, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, nil
}

func (s *ProviderService) GetByKey(providerKey string) (*models.Provider, error) {
	var p models.Provider
	err := s.db.QueryRow(
		"SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, created_at, updated_at FROM providers WHERE provider_key = ? AND is_active = 1",
		providerKey,
	).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProviderService) GetByID(id int64) (*models.Provider, error) {
	var p models.Provider
	err := s.db.QueryRow(
		"SELECT id, provider_key, name, api_base_url, api_key_encrypted, provider_type, is_active, created_at, updated_at FROM providers WHERE id = ?",
		id,
	).Scan(&p.ID, &p.ProviderKey, &p.Name, &p.APiBaseURL, &p.APIKeyEncrypted, &p.ProviderType, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ProviderService) Create(req models.CreateProviderRequest) (*models.Provider, error) {
	cfg := config.Load()

	encrypted, err := crypto.Encrypt(req.APIKey, cfg.EncryptKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt API key: %w", err)
	}

	result, err := s.db.Exec(
		"INSERT INTO providers (provider_key, name, api_base_url, api_key_encrypted, provider_type) VALUES (?, ?, ?, ?, ?)",
		req.ProviderKey, req.Name, req.APiBaseURL, encrypted, req.ProviderType,
	)
	if err != nil {
		return nil, errors.New("provider_key already exists")
	}

	id, _ := result.LastInsertId()
	return s.GetByID(id)
}

func (s *ProviderService) Update(id int64, req models.UpdateProviderRequest) (*models.Provider, error) {
	existing, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}

	name := existing.Name
	apiBaseURL := existing.APiBaseURL
	providerType := existing.ProviderType
	isActive := existing.IsActive

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

	if req.APIKey != nil && *req.APIKey != "" {
		cfg := config.Load()
		encrypted, err := crypto.Encrypt(*req.APIKey, cfg.EncryptKey)
		if err != nil {
			return nil, err
		}
		_, err = s.db.Exec(
			"UPDATE providers SET name=?, api_base_url=?, api_key_encrypted=?, provider_type=?, is_active=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
			name, apiBaseURL, encrypted, providerType, isActive, id,
		)
		if err != nil {
			return nil, err
		}
	} else {
		_, err = s.db.Exec(
			"UPDATE providers SET name=?, api_base_url=?, provider_type=?, is_active=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
			name, apiBaseURL, providerType, isActive, id,
		)
		if err != nil {
			return nil, err
		}
	}

	return s.GetByID(id)
}

func (s *ProviderService) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM models WHERE provider_id = ?", id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("DELETE FROM providers WHERE id = ?", id)
	return err
}

func (s *ProviderService) DecryptAPIKey(encrypted string) (string, error) {
	cfg := config.Load()
	return crypto.Decrypt(encrypted, cfg.EncryptKey)
}

type openaiModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (s *ProviderService) FetchModelsFromProvider(provider *models.Provider) ([]string, error) {
	apiKey, err := s.DecryptAPIKey(provider.APIKeyEncrypted)
	if err != nil {
		return nil, err
	}

	var url string
	switch provider.ProviderType {
	case "openai", "lmstudio", "ollama", "gemini":
		url = provider.APiBaseURL + "/models"
	case "anthropic":
		url = provider.APiBaseURL + "/v1/models?limit=1000"
	default:
		url = provider.APiBaseURL + "/models"
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var listResp openaiModelListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, err
	}

	var modelIDs []string
	for _, m := range listResp.Data {
		modelIDs = append(modelIDs, m.ID)
	}

	return modelIDs, nil
}
