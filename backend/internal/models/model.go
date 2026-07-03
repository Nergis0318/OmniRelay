package models

import "time"

type Model struct {
	ID                        int64     `json:"id"`
	ProviderID                int64     `json:"provider_id"`
	ModelID                   string    `json:"model_id"`
	DisplayName               string    `json:"display_name"`
	ProviderKey               string    `json:"provider_key"`
	IsManual                  bool      `json:"is_manual"`
	SourceProviderKey         string    `json:"source_provider_key"`
	InputPricePer1MTok        float64   `json:"input_price_per_1mtok"`
	OutputPricePer1MTok       float64   `json:"output_price_per_1mtok"`
	CacheWrite5mPricePer1MTok float64   `json:"cache_write_5m_price_per_1mtok"`
	CacheWrite1hPricePer1MTok float64   `json:"cache_write_1h_price_per_1mtok"`
	CacheReadPricePer1MTok    float64   `json:"cache_read_price_per_1mtok"`
	ContextWindow             int64     `json:"context_window"`
	UserID                    int64     `json:"user_id"`
	CreatedAt                 time.Time `json:"created_at"`
}

type CreateModelRequest struct {
	ModelID                   string  `json:"model_id" binding:"required"`
	DisplayName               string  `json:"display_name"`
	ProviderID                int64   `json:"provider_id" binding:"required"`
	InputPricePer1MTok        float64 `json:"input_price_per_1mtok"`
	OutputPricePer1MTok       float64 `json:"output_price_per_1mtok"`
	CacheWrite5mPricePer1MTok float64 `json:"cache_write_5m_price_per_1mtok"`
	CacheWrite1hPricePer1MTok float64 `json:"cache_write_1h_price_per_1mtok"`
	CacheReadPricePer1MTok    float64 `json:"cache_read_price_per_1mtok"`
	ContextWindow             int64   `json:"context_window"`
}

type UpdateModelRequest struct {
	DisplayName               *string  `json:"display_name"`
	InputPricePer1MTok        *float64 `json:"input_price_per_1mtok"`
	OutputPricePer1MTok       *float64 `json:"output_price_per_1mtok"`
	CacheWrite5mPricePer1MTok *float64 `json:"cache_write_5m_price_per_1mtok"`
	CacheWrite1hPricePer1MTok *float64 `json:"cache_write_1h_price_per_1mtok"`
	CacheReadPricePer1MTok    *float64 `json:"cache_read_price_per_1mtok"`
	ContextWindow             *int64   `json:"context_window"`
}

type PublicModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (m *Model) ToPublicModel() PublicModel {
	fullID := m.ProviderKey + "/" + m.ModelID
	return PublicModel{
		ID:      fullID,
		Object:  "model",
		Created: m.CreatedAt.Unix(),
		OwnedBy: m.ProviderKey,
	}
}
