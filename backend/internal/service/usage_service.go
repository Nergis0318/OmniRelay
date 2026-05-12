package service

import (
	"database/sql"
	"fmt"
	"omnirelay/internal/models"
	"strings"
	"time"
)

type UsageService struct {
	db *sql.DB
}

func NewUsageService(db *sql.DB) *UsageService {
	return &UsageService{db: db}
}

func (s *UsageService) Log(log models.UsageLog) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_logs (api_key_id, provider_id, model, request_tokens, response_tokens, total_tokens, latency_ms, cost, is_error, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.APIKeyID, log.ProviderID, log.Model, log.RequestTokens, log.ResponseTokens, log.TotalTokens, log.LatencyMs, log.Cost, log.IsError, log.ErrorMessage,
	)
	return err
}

func (s *UsageService) Query(params models.UsageQueryParams) ([]models.UsageLog, int64, error) {
	where := []string{"1=1"}
	args := []interface{}{}

	if params.APIKeyID != nil {
		where = append(where, "api_key_id = ?")
		args = append(args, *params.APIKeyID)
	}
	if params.ProviderID != nil {
		where = append(where, "provider_id = ?")
		args = append(args, *params.ProviderID)
	}
	if params.Model != "" {
		where = append(where, "model LIKE ?")
		args = append(args, "%"+params.Model+"%")
	}
	if params.From != "" {
		where = append(where, "created_at >= ?")
		args = append(args, params.From)
	}
	if params.To != "" {
		where = append(where, "created_at <= ?")
		args = append(args, params.To)
	}

	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	whereClause := strings.Join(where, " AND ")

	var total int64
	countQuery := "SELECT COUNT(*) FROM usage_logs WHERE " + whereClause
	s.db.QueryRow(countQuery, args...).Scan(&total)

	query := fmt.Sprintf("SELECT id, api_key_id, provider_id, model, request_tokens, response_tokens, total_tokens, latency_ms, cost, is_error, COALESCE(error_message,''), created_at FROM usage_logs WHERE %s ORDER BY created_at DESC LIMIT ? OFFSET ?", whereClause)
	queryArgs := append(args, limit, offset)

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []models.UsageLog
	for rows.Next() {
		var l models.UsageLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.ProviderID, &l.Model, &l.RequestTokens, &l.ResponseTokens, &l.TotalTokens, &l.LatencyMs, &l.Cost, &l.IsError, &l.ErrorMessage, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	return logs, total, nil
}

func (s *UsageService) GetStats() (*models.DashboardStats, error) {
	stats := &models.DashboardStats{}

	s.db.QueryRow("SELECT COUNT(*) FROM usage_logs").Scan(&stats.TotalRequests)
	s.db.QueryRow("SELECT COALESCE(SUM(total_tokens), 0) FROM usage_logs").Scan(&stats.TotalTokens)
	s.db.QueryRow("SELECT COALESCE(SUM(cost), 0) FROM usage_logs").Scan(&stats.TotalCost)
	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM usage_logs WHERE is_error = 0").Scan(&stats.AvgLatencyMs)
	s.db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE is_active = 1").Scan(&stats.ActiveKeys)
	s.db.QueryRow("SELECT COUNT(*) FROM providers WHERE is_active = 1").Scan(&stats.ProvidersCount)
	s.db.QueryRow("SELECT COUNT(*) FROM models m JOIN providers p ON m.provider_id = p.id WHERE p.is_active = 1").Scan(&stats.ModelsCount)

	rows, err := s.db.Query(`
		SELECT DATE(created_at) as date, COALESCE(SUM(total_tokens),0), COALESCE(SUM(cost),0), COUNT(*)
		FROM usage_logs
		WHERE created_at >= DATE('now', '-30 days')
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var d models.DailyUsage
		if err := rows.Scan(&d.Date, &d.TotalTokens, &d.TotalCost, &d.RequestCount); err != nil {
			return nil, err
		}
		stats.DailyUsage = append(stats.DailyUsage, d)
	}
	if stats.DailyUsage == nil {
		stats.DailyUsage = []models.DailyUsage{}
	}

	return stats, nil
}

var _ = time.Now
