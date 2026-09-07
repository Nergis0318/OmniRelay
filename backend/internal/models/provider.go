package models

import "time"

type ProviderEndpoint struct {
	APIType string `json:"api_type"`
	BaseURL string `json:"base_url"`
}

type ProviderAPIKeyPublic struct {
	ID        int64     `json:"id"`
	KeyPrefix string    `json:"key_prefix"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type Provider struct {
	ID              int64                  `json:"id"`
	ProviderKey     string                 `json:"provider_key"`
	Name            string                 `json:"name"`
	APiBaseURL      string                 `json:"api_base_url"`
	APIKeyEncrypted string                 `json:"-"`
	ProviderType    string                 `json:"provider_type"`
	IsActive        bool                   `json:"is_active"`
	ShowInModelList bool                   `json:"show_in_model_list"`
	SourceModels    []string               `json:"source_models,omitempty"`
	Endpoints       []ProviderEndpoint     `json:"endpoints,omitempty"`
	APIKeys         []ProviderAPIKeyPublic `json:"api_keys,omitempty"`
	UserID          int64                  `json:"user_id"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

type CreateProviderRequest struct {
	ProviderKey     string             `json:"provider_key" binding:"required"`
	Name            string             `json:"name" binding:"required"`
	APiBaseURL      string             `json:"api_base_url"`
	APIKey          string             `json:"api_key"`
	ProviderType    string             `json:"provider_type" binding:"required,oneof=openai anthropic lmstudio ollama gemini custom"`
	SourceModels    []string           `json:"source_models"`
	Endpoints       []ProviderEndpoint `json:"endpoints"`
	ShowInModelList *bool              `json:"show_in_model_list"`
}

type UpdateProviderRequest struct {
	Name            *string             `json:"name"`
	APiBaseURL      *string             `json:"api_base_url"`
	APIKey          *string             `json:"api_key"`
	ProviderType    *string             `json:"provider_type" binding:"omitempty,oneof=openai anthropic lmstudio ollama gemini custom"`
	IsActive        *bool               `json:"is_active"`
	SourceModels    *[]string           `json:"source_models"`
	Endpoints       *[]ProviderEndpoint `json:"endpoints"`
	ShowInModelList *bool               `json:"show_in_model_list"`
}
