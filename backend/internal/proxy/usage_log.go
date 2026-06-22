package proxy

import (
	"log"
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

func (e *Engine) persistUsage(entry models.UsageLog) {
	if err := e.usageService.Log(entry); err != nil {
		log.Printf("failed to write usage log: %v", err)
	}
}

func (e *Engine) logUpstreamError(u usageContext, message string, latencyMs int64) {
	entry := u.base()
	entry.IsError = true
	entry.ErrorMessage = message
	entry.LatencyMs = latencyMs
	e.persistUsage(entry)
}

func (e *Engine) logLatencyOnly(u usageContext, latencyMs int64) {
	entry := u.base()
	entry.LatencyMs = latencyMs
	e.persistUsage(entry)
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
	entry := u.base()
	entry.RequestTokens = t.requestTokens
	entry.ResponseTokens = t.responseTokens
	if t.totalTokens > 0 {
		entry.TotalTokens = t.totalTokens
	} else {
		entry.TotalTokens = t.requestTokens + t.responseTokens
	}
	entry.CacheWrite5MTokens = t.cacheWrite5m
	entry.CacheWrite1HTokens = t.cacheWrite1h
	entry.CacheReadTokens = t.cacheRead
	entry.Cost = t.cost
	entry.LatencyMs = t.latencyMs
	entry.StartedAt = t.startedAt
	entry.CompletedAt = t.completedAt
	e.persistUsage(entry)
}
