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
		`INSERT INTO usage_logs (api_key_id, provider_id, model, request_tokens, response_tokens, total_tokens, cache_write_5m_tokens, cache_write_1h_tokens, cache_read_tokens, latency_ms, cost, is_error, error_message, user_id, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.APIKeyID, log.ProviderID, log.Model, log.RequestTokens, log.ResponseTokens, log.TotalTokens,
		log.CacheWrite5MTokens, log.CacheWrite1HTokens, log.CacheReadTokens,
		log.LatencyMs, log.Cost, log.IsError, log.ErrorMessage, log.UserID, log.StartedAt, log.CompletedAt,
	)
	return err
}

func (s *UsageService) Query(params models.UsageQueryParams, userID int64) ([]models.UsageLog, int64, error) {
	where := []string{"(u.user_id = ? OR u.user_id IS NULL)"}
	args := []interface{}{userID}

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
	countQuery := "SELECT COUNT(*) FROM usage_logs u WHERE " + whereClause
	s.db.QueryRow(countQuery, args...).Scan(&total)

	query := fmt.Sprintf("SELECT u.id, u.api_key_id, u.provider_id, u.model, u.request_tokens, u.response_tokens, u.total_tokens, COALESCE(u.cache_write_5m_tokens,0), COALESCE(u.cache_write_1h_tokens,0), COALESCE(u.cache_read_tokens,0), u.latency_ms, u.cost, u.is_error, COALESCE(u.error_message,''), COALESCE(u.user_id, 0), u.started_at, u.completed_at, u.created_at, COALESCE(p.name,'') FROM usage_logs u LEFT JOIN providers p ON u.provider_id = p.id WHERE %s ORDER BY u.created_at DESC LIMIT ? OFFSET ?", whereClause)
	queryArgs := append(args, limit, offset)

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []models.UsageLog
	for rows.Next() {
		var l models.UsageLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.ProviderID, &l.Model, &l.RequestTokens, &l.ResponseTokens, &l.TotalTokens, &l.CacheWrite5MTokens, &l.CacheWrite1HTokens, &l.CacheReadTokens, &l.LatencyMs, &l.Cost, &l.IsError, &l.ErrorMessage, &l.UserID, &l.StartedAt, &l.CompletedAt, &l.CreatedAt, &l.ProviderName); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	return logs, total, nil
}

func (s *UsageService) GetStats(userID int64) (*models.DashboardStats, error) {
	stats := &models.DashboardStats{}

	s.db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE user_id = ?", userID).Scan(&stats.TotalRequests)
	s.db.QueryRow("SELECT COALESCE(SUM(total_tokens), 0) FROM usage_logs WHERE user_id = ?", userID).Scan(&stats.TotalTokens)
	s.db.QueryRow("SELECT COALESCE(SUM(cost), 0) FROM usage_logs WHERE user_id = ?", userID).Scan(&stats.TotalCost)
	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM usage_logs WHERE is_error = 0 AND user_id = ?", userID).Scan(&stats.AvgLatencyMs)
	s.db.QueryRow("SELECT COALESCE(SUM(cache_write_5m_tokens), 0) FROM usage_logs WHERE user_id = ?", userID).Scan(&stats.TotalCacheWrite5M)
	s.db.QueryRow("SELECT COALESCE(SUM(cache_write_1h_tokens), 0) FROM usage_logs WHERE user_id = ?", userID).Scan(&stats.TotalCacheWrite1H)
	s.db.QueryRow("SELECT COALESCE(SUM(cache_read_tokens), 0) FROM usage_logs WHERE user_id = ?", userID).Scan(&stats.TotalCacheRead)
	s.db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE is_active = 1 AND created_by = ?", userID).Scan(&stats.ActiveKeys)
	s.db.QueryRow("SELECT COUNT(*) FROM providers WHERE is_active = 1 AND (user_id = ? OR user_id IS NULL)", userID).Scan(&stats.ProvidersCount)
	s.db.QueryRow("SELECT COUNT(*) FROM models m JOIN providers p ON m.provider_id = p.id WHERE p.is_active = 1 AND (m.user_id = ? OR m.user_id IS NULL)", userID).Scan(&stats.ModelsCount)

	s.db.QueryRow("SELECT COALESCE(SUM(cost), 0) FROM usage_logs WHERE DATE(created_at) = DATE('now') AND (user_id = ? OR user_id IS NULL)", userID).Scan(&stats.TodayCost)
	s.db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE DATE(created_at) = DATE('now') AND (user_id = ? OR user_id IS NULL)", userID).Scan(&stats.TodayRequests)
	s.db.QueryRow("SELECT COALESCE(SUM(total_tokens), 0) FROM usage_logs WHERE DATE(created_at) = DATE('now') AND (user_id = ? OR user_id IS NULL)", userID).Scan(&stats.TodayTokens)

	s.db.QueryRow(`
		SELECT COALESCE(COUNT(*) * 60.0 / MAX(1, CAST((julianday('now') - julianday(MIN(created_at))) * 24 * 60 AS INTEGER)), 0)
		FROM usage_logs
		WHERE created_at >= datetime('now', '-5 minutes') AND (user_id = ? OR user_id IS NULL)
	`, userID).Scan(&stats.RPM)

	s.db.QueryRow(`
		SELECT COALESCE(SUM(total_tokens) * 60.0 / MAX(1, CAST((julianday('now') - julianday(MIN(created_at))) * 24 * 60 AS INTEGER)), 0)
		FROM usage_logs
		WHERE created_at >= datetime('now', '-5 minutes') AND (user_id = ? OR user_id IS NULL)
	`, userID).Scan(&stats.TPM)

	rows, err := s.db.Query(`
		SELECT DATE(created_at) as date, COALESCE(SUM(total_tokens),0), COALESCE(SUM(cost),0), COUNT(*)
		FROM usage_logs
		WHERE created_at >= DATE('now', '-30 days') AND (user_id = ? OR user_id IS NULL)
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`, userID)
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
