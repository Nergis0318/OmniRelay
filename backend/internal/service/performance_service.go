package service

import (
	"database/sql"
	"fmt"
	"math"
	"omnirelay/internal/models"
	"sort"
	"strings"
	"time"
)

type PerformanceService struct {
	db *sql.DB
}

func NewPerformanceService(db *sql.DB) *PerformanceService {
	return &PerformanceService{db: db}
}

func (s *PerformanceService) GetPerformance(userID int64, params models.PerformanceQueryParams) (*models.PerformanceResponse, error) {
	where, args := buildPerfWhere(params, userID, "")

	summary, err := s.querySummary(where, args, params)
	if err != nil {
		return nil, err
	}

	granularity := resolveGranularity(params.Granularity, params.From, params.To)
	timeseries, err := s.queryTimeseries(where, args, granularity)
	if err != nil {
		return nil, err
	}

	// queryByProvider JOINs providers, which also has a user_id column, so
	// the filter clause must be table-qualified to avoid ambiguity.
	joinedWhere, joinedArgs := buildPerfWhere(params, userID, "u")
	byProvider, err := s.queryByProvider(joinedWhere, joinedArgs)
	if err != nil {
		return nil, err
	}

	byModel, err := s.queryByModel(where, args)
	if err != nil {
		return nil, err
	}

	topByCost, err := s.queryTopByCost(where, args)
	if err != nil {
		return nil, err
	}

	if timeseries == nil {
		timeseries = []models.PerformanceBucket{}
	}
	if byProvider == nil {
		byProvider = []models.PerformanceBreakdown{}
	}
	if byModel == nil {
		byModel = []models.PerformanceBreakdown{}
	}
	if topByCost == nil {
		topByCost = []models.PerformanceBreakdown{}
	}

	return &models.PerformanceResponse{
		Summary:         *summary,
		Timeseries:      timeseries,
		ByProvider:      byProvider,
		ByModel:         byModel,
		TopModelsByCost: topByCost,
	}, nil
}

// buildPerfWhere builds the shared filter clause. alias (e.g. "u") prefixes
// column names so the clause also works in JOINed queries without ambiguity.
func buildPerfWhere(params models.PerformanceQueryParams, userID int64, alias string) (string, []interface{}) {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	clauses := []string{prefix + "user_id = ?"}
	args := []interface{}{userID}
	if params.ProviderID != nil {
		clauses = append(clauses, prefix+"provider_id = ?")
		args = append(args, *params.ProviderID)
	}
	if params.From != "" {
		clauses = append(clauses, prefix+"created_at >= ?")
		args = append(args, params.From)
	}
	if params.To != "" {
		clauses = append(clauses, prefix+"created_at <= ?")
		args = append(args, params.To)
	}
	return strings.Join(clauses, " AND "), args
}

func (s *PerformanceService) querySummary(where string, args []interface{}, params models.PerformanceQueryParams) (*models.PerformanceSummary, error) {
	summary := &models.PerformanceSummary{}

	var avgTTFT sql.NullFloat64
	var ttftCount int64
	var totalTokens, cacheReadTokens sql.NullInt64
	var totalRequests int64

	row := s.db.QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(total_tokens),0),
		        COALESCE(AVG(CASE WHEN is_error = 0 THEN latency_ms END),0),
		        COALESCE(SUM(CASE WHEN is_error = 1 THEN 1 ELSE 0 END),0),
		        AVG(CASE WHEN ttft_ms IS NOT NULL THEN ttft_ms END),
		        COUNT(CASE WHEN ttft_ms IS NOT NULL THEN 1 END),
		        COALESCE(SUM(cache_read_tokens),0)
		 FROM usage_logs WHERE `+where,
		args...,
	)
	if err := row.Scan(&totalRequests, &totalTokens, &summary.AvgLatencyMs, &summary.ErrorRate, &avgTTFT, &ttftCount, &cacheReadTokens); err != nil {
		return nil, err
	}

	summary.TotalRequests = totalRequests
	summary.TTFTCount = ttftCount
	if avgTTFT.Valid {
		v := avgTTFT.Float64
		summary.AvgTTFTMs = &v
	}
	if totalRequests > 0 {
		summary.ErrorRate = summary.ErrorRate / float64(totalRequests)
	}
	if totalTokens.Valid && totalTokens.Int64 > 0 && cacheReadTokens.Valid {
		summary.CacheHitRate = float64(cacheReadTokens.Int64) / float64(totalTokens.Int64)
	}

	elapsedMin := s.elapsedMinutes(where, args, params)
	if elapsedMin > 0 {
		summary.RPM = float64(totalRequests) / elapsedMin
		if totalTokens.Valid {
			summary.TPM = float64(totalTokens.Int64) / elapsedMin
		}
	}

	p50, p95, p99, err := s.queryPercentiles(where, args)
	if err != nil {
		return nil, err
	}
	summary.P50Ms = p50
	summary.P95Ms = p95
	summary.P99Ms = p99

	return summary, nil
}

func (s *PerformanceService) elapsedMinutes(where string, args []interface{}, params models.PerformanceQueryParams) float64 {
	if params.From != "" && params.To != "" {
		if mins := windowMinutes(params.From, params.To); mins > 0 {
			return mins
		}
	}

	var minCreated string
	if err := s.db.QueryRow(`SELECT MIN(created_at) FROM usage_logs WHERE `+where, args...).Scan(&minCreated); err != nil || minCreated == "" {
		return 0
	}
	if mins := windowMinutes(minCreated, time.Now().UTC().Format("2006-01-02 15:04:05")); mins > 0 {
		return mins
	}
	return 0
}

func windowMinutes(from, to string) float64 {
	tFrom, err := parseAnyTime(from)
	if err != nil {
		return 0
	}
	tTo, err := parseAnyTime(to)
	if err != nil {
		return 0
	}
	mins := tTo.Sub(tFrom).Minutes()
	if mins < 1 {
		mins = 1
	}
	return mins
}

func parseAnyTime(v string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339,
		"2006-01-02",
	}
	var lastErr error
	for _, l := range layouts {
		t, err := time.Parse(l, v)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func (s *PerformanceService) queryPercentiles(where string, args []interface{}) (float64, float64, float64, error) {
	query := `SELECT latency_ms FROM usage_logs WHERE ` + where + ` AND is_error = 0 ORDER BY latency_ms ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	var latencies []float64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return 0, 0, 0, err
		}
		latencies = append(latencies, float64(v))
		if len(latencies) >= 50000 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	if len(latencies) == 0 {
		return 0, 0, 0, nil
	}

	// Sample if truncated: database returns sorted, so we already have lowest 50000.
	// For large datasets this biases low; acceptable tradeoff for SQLite without percentile func.
	sort.Float64s(latencies)
	return percentile(latencies, 0.50), percentile(latencies, 0.95), percentile(latencies, 0.99), nil
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func resolveGranularity(input, from, to string) string {
	switch input {
	case "minute", "hour", "day":
		return input
	}
	if from != "" && to != "" {
		mins := windowMinutes(from, to)
		if mins <= 120 {
			return "minute"
		}
		if mins <= 7*1440 {
			return "hour"
		}
	}
	return "day"
}

func bucketFormat(granularity string) string {
	switch granularity {
	case "minute":
		return "%Y-%m-%d %H:%M"
	case "hour":
		return "%Y-%m-%d %H:00"
	default:
		return "%Y-%m-%d"
	}
}

func bucketMinutes(granularity string) float64 {
	switch granularity {
	case "minute":
		return 1
	case "hour":
		return 60
	default:
		return 1440
	}
}

func (s *PerformanceService) queryTimeseries(where string, args []interface{}, granularity string) ([]models.PerformanceBucket, error) {
	fmt2 := bucketFormat(granularity)
	bMins := bucketMinutes(granularity)

	query := fmt.Sprintf(
		`SELECT strftime('%s', created_at) AS bucket,
		        COUNT(*) as cnt,
		        COALESCE(SUM(total_tokens),0),
		        COALESCE(AVG(CASE WHEN is_error = 0 THEN latency_ms END),0),
		        AVG(CASE WHEN ttft_ms IS NOT NULL THEN ttft_ms END),
		        SUM(CASE WHEN is_error = 1 THEN 1 ELSE 0 END)
		 FROM usage_logs WHERE %s GROUP BY bucket ORDER BY bucket ASC LIMIT 500`, fmt2, where)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []models.PerformanceBucket
	for rows.Next() {
		var b models.PerformanceBucket
		var totalTokens int64
		var avgTTFT sql.NullFloat64
		if err := rows.Scan(&b.Bucket, &b.RequestCount, &totalTokens, &b.AvgLatencyMs, &avgTTFT, &b.ErrorCount); err != nil {
			return nil, err
		}
		b.RPM = float64(b.RequestCount) / bMins
		b.TPM = float64(totalTokens) / bMins
		if avgTTFT.Valid {
			v := avgTTFT.Float64
			b.AvgTTFTMs = &v
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

func (s *PerformanceService) queryByProvider(where string, args []interface{}) ([]models.PerformanceBreakdown, error) {
	query := `SELECT u.provider_id, COALESCE(p.name,''), COUNT(*) as cnt, COALESCE(SUM(u.total_tokens),0), COALESCE(AVG(CASE WHEN u.is_error=0 THEN u.latency_ms END),0), COALESCE(SUM(u.cost),0)
	          FROM usage_logs u LEFT JOIN providers p ON u.provider_id = p.id
	          WHERE ` + where + ` GROUP BY u.provider_id ORDER BY cnt DESC LIMIT 10`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.PerformanceBreakdown
	for rows.Next() {
		var b models.PerformanceBreakdown
		if err := rows.Scan(&b.ProviderID, &b.ProviderName, &b.Requests, &b.Tokens, &b.AvgLatencyMs, &b.Cost); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (s *PerformanceService) queryByModel(where string, args []interface{}) ([]models.PerformanceBreakdown, error) {
	query := `SELECT NULL, '', model, COUNT(*) as cnt, COALESCE(SUM(total_tokens),0), COALESCE(AVG(CASE WHEN is_error=0 THEN latency_ms END),0), COALESCE(SUM(cost),0)
	          FROM usage_logs WHERE ` + where + ` GROUP BY model ORDER BY cnt DESC LIMIT 10`
	// model breakdown: provider fields unused, model field populated
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.PerformanceBreakdown
	for rows.Next() {
		var b models.PerformanceBreakdown
		if err := rows.Scan(&b.ProviderID, &b.ProviderName, &b.Model, &b.Requests, &b.Tokens, &b.AvgLatencyMs, &b.Cost); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (s *PerformanceService) queryTopByCost(where string, args []interface{}) ([]models.PerformanceBreakdown, error) {
	query := `SELECT NULL, '', model, COUNT(*) as cnt, COALESCE(SUM(total_tokens),0), COALESCE(AVG(CASE WHEN is_error=0 THEN latency_ms END),0), COALESCE(SUM(cost),0)
	          FROM usage_logs WHERE ` + where + ` GROUP BY model ORDER BY SUM(cost) DESC LIMIT 10`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.PerformanceBreakdown
	for rows.Next() {
		var b models.PerformanceBreakdown
		if err := rows.Scan(&b.ProviderID, &b.ProviderName, &b.Model, &b.Requests, &b.Tokens, &b.AvgLatencyMs, &b.Cost); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}
