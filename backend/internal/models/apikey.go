package models

import "time"

type APIKey struct {
	ID          int64      `json:"id"`
	KeyHash     string     `json:"-"`
	KeyPrefix   string     `json:"key_prefix"`
	Name        string     `json:"name"`
	CreatedBy   int64      `json:"created_by"`
	IsActive    bool       `json:"is_active"`
	RateLimitRPM int64     `json:"rate_limit_rpm"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

type CreateAPIKeyRequest struct {
	Name        string `json:"name" binding:"required"`
	RateLimitRPM int64 `json:"rate_limit_rpm"`
}

type CreateAPIKeyResponse struct {
	APIKey    APIKey `json:"api_key"`
	PlainKey  string `json:"plain_key"`
}
