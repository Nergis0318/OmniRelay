package proxy

import (
	"omnirelay/internal/models"
	"time"
)

// usageContext bundles the identifiers that every UsageLog row needs.
type usageContext struct {
	apiKeyID    int64
	providerID  int64
	userID      int64
	fullModelID string
}

func (u usageContext) base() models.UsageLog {
	apiKeyID := u.apiKeyID
	providerID := u.providerID
	userID := u.userID
	return models.UsageLog{
		APIKeyID:   &apiKeyID,
		ProviderID: &providerID,
		UserID:     &userID,
		Model:      u.fullModelID,
	}
}

func (e *Engine) logUpstreamError(u usageContext, message string, latencyMs int64) {
	log := u.base()
	log.IsError = true
	log.ErrorMessage = message
	log.LatencyMs = latencyMs
	e.usageService.Log(log)
}

func (e *Engine) logLatencyOnly(u usageContext, latencyMs int64) {
	log := u.base()
	log.LatencyMs = latencyMs
	e.usageService.Log(log)
}

// tokenUsage carries all of the numeric counters we may want to log for a successful call.
type tokenUsage struct {
	requestTokens  int64
	responseTokens int64
	totalTokens    int64
	cacheWrite5m   int64
	cacheWrite1h   int64
	cacheRead      int64
	cost           float64
	startedAt      *time.Time
	completedAt    *time.Time
	latencyMs      int64
}

func (e *Engine) logTokenUsage(u usageContext, t tokenUsage) {
	log := u.base()
	log.RequestTokens = t.requestTokens
	log.ResponseTokens = t.responseTokens
	if t.totalTokens > 0 {
		log.TotalTokens = t.totalTokens
	} else {
		log.TotalTokens = t.requestTokens + t.responseTokens
	}
	log.CacheWrite5MTokens = t.cacheWrite5m
	log.CacheWrite1HTokens = t.cacheWrite1h
	log.CacheReadTokens = t.cacheRead
	log.Cost = t.cost
	log.LatencyMs = t.latencyMs
	log.StartedAt = t.startedAt
	log.CompletedAt = t.completedAt
	e.usageService.Log(log)
}
