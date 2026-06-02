package service

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"omnirelay/internal/models"
	"time"
)

var ErrRateLimitExceeded = errors.New("rate limit exceeded")

type APIKeyService struct {
	db *sql.DB
}

func NewAPIKeyService(db *sql.DB) *APIKeyService {
	return &APIKeyService{db: db}
}

func (s *APIKeyService) List(userID int64) ([]models.APIKey, error) {
	rows, err := s.db.Query(
		"SELECT id, key_prefix, name, created_by, is_active, rate_limit_rpm, created_at, last_used_at FROM api_keys WHERE created_by = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.KeyPrefix, &k.Name, &k.CreatedBy, &k.IsActive, &k.RateLimitRPM, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *APIKeyService) Create(req models.CreateAPIKeyRequest, userID int64) (*models.CreateAPIKeyResponse, error) {
	plainKey, prefix, hash, err := generateAPIKey()
	if err != nil {
		return nil, err
	}

	result, err := s.db.Exec(
		"INSERT INTO api_keys (key_hash, key_prefix, name, created_by, rate_limit_rpm) VALUES (?, ?, ?, ?, ?)",
		hash, prefix, req.Name, userID, req.RateLimitRPM,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()

	apiKey := models.APIKey{
		ID:           id,
		KeyPrefix:    prefix,
		Name:         req.Name,
		CreatedBy:    userID,
		IsActive:     true,
		RateLimitRPM: req.RateLimitRPM,
		CreatedAt:    time.Now(),
	}

	return &models.CreateAPIKeyResponse{
		APIKey:   apiKey,
		PlainKey: plainKey,
	}, nil
}

func (s *APIKeyService) Validate(plainKey string) (*models.APIKey, error) {
	hash := hashKey(plainKey)

	var k models.APIKey
	err := s.db.QueryRow(
		"SELECT id, key_prefix, name, created_by, is_active, rate_limit_rpm, created_at, last_used_at FROM api_keys WHERE key_hash = ?",
		hash,
	).Scan(&k.ID, &k.KeyPrefix, &k.Name, &k.CreatedBy, &k.IsActive, &k.RateLimitRPM, &k.CreatedAt, &k.LastUsedAt)
	if err != nil {
		return nil, errors.New("invalid API key")
	}

	if !k.IsActive {
		return nil, errors.New("API key is inactive")
	}
	if err := s.checkRateLimit(k); err != nil {
		return nil, err
	}

	s.db.Exec("UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", k.ID)

	return &k, nil
}

func (s *APIKeyService) Delete(id int64, userID int64) error {
	_, err := s.db.Exec("UPDATE api_keys SET is_active = 0 WHERE id = ? AND created_by = ?", id, userID)
	return err
}

func (s *APIKeyService) CountActive(userID int64) (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE is_active = 1 AND created_by = ?", userID).Scan(&count)
	return count, err
}

func (s *APIKeyService) checkRateLimit(k models.APIKey) error {
	if k.RateLimitRPM <= 0 {
		return nil
	}

	var count int64
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM usage_logs WHERE api_key_id = ? AND created_at >= datetime('now', '-1 minute')",
		k.ID,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count >= k.RateLimitRPM {
		return fmt.Errorf("%w: %d requests per minute", ErrRateLimitExceeded, k.RateLimitRPM)
	}
	return nil
}

func generateAPIKey() (plainKey string, prefix string, hash string, err error) {
	bytes := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, bytes); err != nil {
		return "", "", "", err
	}
	randomPart := hex.EncodeToString(bytes)

	plainKey = "om-ni-" + randomPart
	prefix = plainKey[:17]
	hash = hashKey(plainKey)
	return
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
