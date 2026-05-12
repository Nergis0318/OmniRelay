package service

import (
	"database/sql"
	"errors"
	"omnirelay/internal/models"
	"time"
)

type ModelService struct {
	db *sql.DB
}

func NewModelService(db *sql.DB) *ModelService {
	return &ModelService{db: db}
}

func (s *ModelService) List(providerKey string) ([]models.Model, error) {
	query := `SELECT m.id, m.provider_id, m.model_id, COALESCE(m.display_name,''), m.provider_key,
		m.is_manual, m.input_price_per_1mtok, m.output_price_per_1mtok, m.cache_write_5m_price_per_1mtok, m.cache_write_1h_price_per_1mtok, m.cache_read_price_per_1mtok, m.context_window, m.created_at
		FROM models m JOIN providers p ON m.provider_id = p.id WHERE p.is_active = 1`
	args := []interface{}{}

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
			&m.IsManual, &m.InputPricePer1MTok, &m.OutputPricePer1MTok, &m.CacheWrite5mPricePer1MTok, &m.CacheWrite1hPricePer1MTok, &m.CacheReadPricePer1MTok, &m.ContextWindow, &m.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, nil
}

func (s *ModelService) SyncFromProvider(providerID int64, providerKey string, modelIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM models WHERE provider_id = ? AND is_manual = 0", providerID)
	if err != nil {
		return err
	}

	for _, modelID := range modelIDs {
		_, err := tx.Exec(
			"INSERT OR IGNORE INTO models (provider_id, model_id, display_name, provider_key, is_manual) VALUES (?, ?, ?, ?, 0)",
			providerID, modelID, modelID, providerKey,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *ModelService) Create(req models.CreateModelRequest) (*models.Model, error) {
	var providerKey string
	err := s.db.QueryRow("SELECT provider_key FROM providers WHERE id = ?", req.ProviderID).Scan(&providerKey)
	if err != nil {
		return nil, errors.New("provider not found")
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.ModelID
	}

	result, err := s.db.Exec(
		"INSERT INTO models (provider_id, model_id, display_name, provider_key, is_manual, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok, context_window) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)",
		req.ProviderID, req.ModelID, displayName, providerKey, req.InputPricePer1MTok, req.OutputPricePer1MTok, req.CacheWrite5mPricePer1MTok, req.CacheWrite1hPricePer1MTok, req.CacheReadPricePer1MTok, req.ContextWindow,
	)
	if err != nil {
		return nil, errors.New("model already exists for this provider")
	}

	id, _ := result.LastInsertId()
	return s.GetByID(id)
}

func (s *ModelService) GetByID(id int64) (*models.Model, error) {
	var m models.Model
	err := s.db.QueryRow(
		`SELECT id, provider_id, model_id, COALESCE(display_name,''), provider_key,
			is_manual, input_price_per_1mtok, output_price_per_1mtok, cache_write_5m_price_per_1mtok, cache_write_1h_price_per_1mtok, cache_read_price_per_1mtok, context_window, created_at FROM models WHERE id = ?`,
		id,
	).Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.ProviderKey,
		&m.IsManual, &m.InputPricePer1MTok, &m.OutputPricePer1MTok, &m.CacheWrite5mPricePer1MTok, &m.CacheWrite1hPricePer1MTok, &m.CacheReadPricePer1MTok, &m.ContextWindow, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *ModelService) FindByFullID(fullModelID string) (*models.Model, error) {
	for i := 0; i < len(fullModelID); i++ {
		if fullModelID[i] == '/' {
			providerKey := fullModelID[:i]
			modelID := fullModelID[i+1:]

			var m models.Model
			err := s.db.QueryRow(
				`SELECT m.id, m.provider_id, m.model_id, COALESCE(m.display_name,''), m.provider_key,
					m.is_manual, m.input_price_per_1mtok, m.output_price_per_1mtok, m.cache_write_5m_price_per_1mtok, m.cache_write_1h_price_per_1mtok, m.cache_read_price_per_1mtok, m.context_window, m.created_at
				FROM models m JOIN providers p ON m.provider_id = p.id
				WHERE m.provider_key = ? AND m.model_id = ? AND p.is_active = 1`,
				providerKey, modelID,
			).Scan(&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.ProviderKey,
				&m.IsManual, &m.InputPricePer1MTok, &m.OutputPricePer1MTok, &m.CacheWrite5mPricePer1MTok, &m.CacheWrite1hPricePer1MTok, &m.CacheReadPricePer1MTok, &m.ContextWindow, &m.CreatedAt)
			if err != nil {
				return nil, err
			}
			return &m, nil
		}
	}
	return nil, errors.New("invalid model ID format: expected provider/model")
}

func (s *ModelService) Update(id int64, req models.UpdateModelRequest) (*models.Model, error) {
	existing, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}

	displayName := existing.DisplayName
	inputPrice := existing.InputPricePer1MTok
	outputPrice := existing.OutputPricePer1MTok
	cacheWrite5mPrice := existing.CacheWrite5mPricePer1MTok
	cacheWrite1hPrice := existing.CacheWrite1hPricePer1MTok
	cacheHitPrice := existing.CacheReadPricePer1MTok
	ctxWindow := existing.ContextWindow

	if req.DisplayName != nil {
		displayName = *req.DisplayName
	}
	if req.InputPricePer1MTok != nil {
		inputPrice = *req.InputPricePer1MTok
	}
	if req.OutputPricePer1MTok != nil {
		outputPrice = *req.OutputPricePer1MTok
	}
	if req.CacheWrite5mPricePer1MTok != nil {
		cacheWrite5mPrice = *req.CacheWrite5mPricePer1MTok
	}
	if req.CacheWrite1hPricePer1MTok != nil {
		cacheWrite1hPrice = *req.CacheWrite1hPricePer1MTok
	}
	if req.CacheReadPricePer1MTok != nil {
		cacheHitPrice = *req.CacheReadPricePer1MTok
	}
	if req.ContextWindow != nil {
		ctxWindow = *req.ContextWindow
	}

	_, err = s.db.Exec(
		"UPDATE models SET display_name=?, input_price_per_1mtok=?, output_price_per_1mtok=?, cache_write_5m_price_per_1mtok=?, cache_write_1h_price_per_1mtok=?, cache_read_price_per_1mtok=?, context_window=? WHERE id=?",
		displayName, inputPrice, outputPrice, cacheWrite5mPrice, cacheWrite1hPrice, cacheHitPrice, ctxWindow, id,
	)
	if err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

func (s *ModelService) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM models WHERE id = ?", id)
	return err
}

func (s *ModelService) CountActive() (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM models m JOIN providers p ON m.provider_id = p.id WHERE p.is_active = 1").Scan(&count)
	return count, err
}

var _ = time.Now
