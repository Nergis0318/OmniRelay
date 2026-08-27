package models

import "time"

// PassthroughLog is one measured URL-passthrough relay. It records the usage
// the upstream itself reported (nil when it reports none); no cost.
type PassthroughLog struct {
	ID                 int64      `json:"id"`
	Host               string     `json:"host"`
	Path               string     `json:"path"`
	Method             string     `json:"method"`
	Model              string     `json:"model"`
	StatusCode         int        `json:"status_code"`
	IsError            bool       `json:"is_error"`
	ErrMessage         string     `json:"error_message,omitempty"`
	DNSMs              *int64     `json:"dns_ms"`
	ConnectMs          *int64     `json:"connect_ms"`
	TLSMs              *int64     `json:"tls_ms"`
	TTFBMs             *int64     `json:"ttfb_ms"`
	TTFTMs             *int64     `json:"ttft_ms"`
	TotalMs            int64      `json:"total_ms"`
	RequestBytes       int64      `json:"request_bytes"`
	ResponseBytes      int64      `json:"response_bytes"`
	InputTokens        *int64     `json:"input_tokens"`
	OutputTokens       *int64     `json:"output_tokens"`
	CacheWrite5MTokens *int64     `json:"cache_write_5m_tokens"`
	CacheWrite1HTokens *int64     `json:"cache_write_1h_tokens"`
	CacheReadTokens    *int64     `json:"cache_read_tokens"`
	StartedAt          time.Time  `json:"started_at"`
	CreatedAt          *time.Time `json:"created_at,omitempty"`
}

type PassthroughQueryParams struct {
	Host        string `form:"host"`
	From        string `form:"from"`
	To          string `form:"to"`
	Granularity string `form:"granularity"`
	Limit       int    `form:"limit"`
	Offset      int    `form:"offset"`
}

type PassthroughSummary struct {
	TotalRequests         int64    `json:"total_requests"`
	ErrorRate             float64  `json:"error_rate"`
	RequestsPerSec        float64  `json:"requests_per_sec"`
	AvgTotalMs            *float64 `json:"avg_total_ms"`
	P50TotalMs            *float64 `json:"p50_total_ms"`
	P95TotalMs            *float64 `json:"p95_total_ms"`
	P99TotalMs            *float64 `json:"p99_total_ms"`
	AvgTTFBMs             *float64 `json:"avg_ttfb_ms"`
	AvgTTFTMs             *float64 `json:"avg_ttft_ms"`
	AvgDNSMs              *float64 `json:"avg_dns_ms"`
	AvgConnectMs          *float64 `json:"avg_connect_ms"`
	AvgTLSMs              *float64 `json:"avg_tls_ms"`
	AvgResponseBytes      float64  `json:"avg_response_bytes"`
	TotalInputTokens      *int64   `json:"total_input_tokens"`
	TotalOutputTokens     *int64   `json:"total_output_tokens"`
	TotalCacheWrite5MToks *int64   `json:"total_cache_write_5m_tokens"`
	TotalCacheWrite1HToks *int64   `json:"total_cache_write_1h_tokens"`
	TotalCacheReadTokens  *int64   `json:"total_cache_read_tokens"`
}

type PassthroughBucket struct {
	Bucket           string   `json:"bucket"`
	RequestCount     int64    `json:"request_count"`
	ErrorCount       int64    `json:"error_count"`
	AvgTotalMs       *float64 `json:"avg_total_ms"`
	MaxTotalMs       int64    `json:"max_total_ms"`
	AvgTTFBMs        *float64 `json:"avg_ttfb_ms"`
	AvgResponseBytes float64  `json:"avg_response_bytes"`
	InputTokens      *int64   `json:"input_tokens"`
	OutputTokens     *int64   `json:"output_tokens"`
	CacheWrite5MToks *int64   `json:"cache_write_5m_tokens"`
	CacheWrite1HToks *int64   `json:"cache_write_1h_tokens"`
	CacheReadTokens  *int64   `json:"cache_read_tokens"`
}

type PassthroughHostStats struct {
	Host             string   `json:"host"`
	Requests         int64    `json:"requests"`
	Errors           int64    `json:"errors"`
	AvgTotalMs       *float64 `json:"avg_total_ms"`
	AvgTTFBMs        *float64 `json:"avg_ttfb_ms"`
	AvgTTFTMs        *float64 `json:"avg_ttft_ms"`
	AvgResponseBytes float64  `json:"avg_response_bytes"`
	OutputTokens     *int64   `json:"output_tokens"`
}

type PassthroughPerformanceResponse struct {
	Summary    PassthroughSummary     `json:"summary"`
	Timeseries []PassthroughBucket    `json:"timeseries"`
	ByHost     []PassthroughHostStats `json:"by_host"`
}
