package models

type PerformanceQueryParams struct {
	ProviderID  *int64 `form:"provider_id"`
	From        string `form:"from"`
	To          string `form:"to"`
	Granularity string `form:"granularity"`
}

type PerformanceSummary struct {
	TotalRequests int64    `json:"total_requests"`
	RPM           float64  `json:"rpm"`
	TPM           float64  `json:"tpm"`
	AvgLatencyMs  float64  `json:"avg_latency_ms"`
	P50Ms         float64  `json:"p50_ms"`
	P95Ms         float64  `json:"p95_ms"`
	P99Ms         float64  `json:"p99_ms"`
	AvgTTFTMs     *float64 `json:"avg_ttft_ms"`
	TTFTCount     int64    `json:"ttft_count"`
	ErrorRate     float64  `json:"error_rate"`
	CacheHitRate  float64  `json:"cache_hit_rate"`
}

type PerformanceBucket struct {
	Bucket       string   `json:"bucket"`
	RequestCount int64    `json:"request_count"`
	RPM          float64  `json:"rpm"`
	TPM          float64  `json:"tpm"`
	AvgLatencyMs float64  `json:"avg_latency_ms"`
	AvgTTFTMs    *float64 `json:"avg_ttft_ms"`
	ErrorCount   int64    `json:"error_count"`
}

type PerformanceBreakdown struct {
	ProviderID   *int64  `json:"provider_id"`
	ProviderName string  `json:"provider_name"`
	Model        string  `json:"model"`
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	Cost         float64 `json:"cost"`
}

type PerformanceResponse struct {
	Summary         PerformanceSummary     `json:"summary"`
	Timeseries      []PerformanceBucket    `json:"timeseries"`
	ByProvider      []PerformanceBreakdown `json:"by_provider"`
	ByModel         []PerformanceBreakdown `json:"by_model"`
	TopModelsByCost []PerformanceBreakdown `json:"top_models_by_cost"`
}
