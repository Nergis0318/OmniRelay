package models

import "time"

type UsageLog struct {
	ID              int64     `json:"id"`
	APIKeyID        *int64    `json:"api_key_id"`
	ProviderID      *int64    `json:"provider_id"`
	Model           string    `json:"model"`
	RequestTokens   int64     `json:"request_tokens"`
	ResponseTokens  int64     `json:"response_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	LatencyMs       int64     `json:"latency_ms"`
	Cost            float64   `json:"cost"`
	IsError         bool      `json:"is_error"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type UsageQueryParams struct {
	APIKeyID   *int64  `form:"api_key_id"`
	ProviderID *int64  `form:"provider_id"`
	Model      string  `form:"model"`
	From       string  `form:"from"`
	To         string  `form:"to"`
	Limit      int     `form:"limit"`
	Offset     int     `form:"offset"`
}

type DashboardStats struct {
	TotalRequests    int64        `json:"total_requests"`
	TotalTokens      int64        `json:"total_tokens"`
	TotalCost        float64      `json:"total_cost"`
	AvgLatencyMs     float64      `json:"avg_latency_ms"`
	ActiveKeys       int64        `json:"active_keys"`
	ProvidersCount   int64        `json:"providers_count"`
	ModelsCount      int64        `json:"models_count"`
	DailyUsage       []DailyUsage `json:"daily_usage"`
}

type DailyUsage struct {
	Date         string  `json:"date"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCost    float64 `json:"total_cost"`
	RequestCount int64   `json:"request_count"`
}
