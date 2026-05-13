package models

import "time"

type Provider struct {
	ID              int64     `json:"id"`
	ProviderKey     string    `json:"provider_key"`
	Name            string    `json:"name"`
	APiBaseURL      string    `json:"api_base_url"`
	APIKeyEncrypted string    `json:"-"`
	ProviderType    string    `json:"provider_type"`
	IsActive        bool      `json:"is_active"`
	UserID          int64     `json:"user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateProviderRequest struct {
	ProviderKey  string `json:"provider_key" binding:"required"`
	Name         string `json:"name" binding:"required"`
	APiBaseURL   string `json:"api_base_url" binding:"required"`
	APIKey       string `json:"api_key" binding:"required"`
	ProviderType string `json:"provider_type" binding:"required,oneof=openai anthropic lmstudio ollama gemini"`
}

type UpdateProviderRequest struct {
	Name         *string `json:"name"`
	APiBaseURL   *string `json:"api_base_url"`
	APIKey       *string `json:"api_key"`
	ProviderType *string `json:"provider_type" binding:"omitempty,oneof=openai anthropic lmstudio ollama gemini"`
	IsActive     *bool   `json:"is_active"`
}
