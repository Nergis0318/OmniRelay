package service

import (
	"path/filepath"
	"testing"

	"omnirelay/internal/database"
	"omnirelay/internal/models"
)

func newTestUsageService(t *testing.T) *UsageService {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewUsageService(db)
}

func insertUsageLog(t *testing.T, svc *UsageService, userID *int64, totalTokens int64, cost float64) {
	t.Helper()
	_, err := svc.db.Exec(
		`INSERT INTO usage_logs (model, total_tokens, cost, user_id, created_at)
		 VALUES ('test/model', ?, ?, ?, datetime('now'))`,
		totalTokens, cost, userID,
	)
	if err != nil {
		t.Fatalf("insert usage log: %v", err)
	}
}

func TestGetStatsScopesByUser(t *testing.T) {
	svc := newTestUsageService(t)
	u1 := int64(1)
	u2 := int64(2)

	insertUsageLog(t, svc, &u1, 100, 1.0)
	insertUsageLog(t, svc, &u2, 200, 2.0)
	insertUsageLog(t, svc, nil, 999, 9.0)

	stats, err := svc.GetStats(1)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalRequests != 1 {
		t.Errorf("user 1 total_requests = %d, want 1", stats.TotalRequests)
	}
	if stats.TotalTokens != 100 {
		t.Errorf("user 1 total_tokens = %d, want 100", stats.TotalTokens)
	}
	if stats.TotalCost != 1.0 {
		t.Errorf("user 1 total_cost = %v, want 1.0", stats.TotalCost)
	}
	if stats.TodayRequests != 1 {
		t.Errorf("user 1 today_requests = %d, want 1", stats.TodayRequests)
	}
	if stats.TodayTokens != 100 {
		t.Errorf("user 1 today_tokens = %d, want 100", stats.TodayTokens)
	}
	if stats.TodayCost != 1.0 {
		t.Errorf("user 1 today_cost = %v, want 1.0", stats.TodayCost)
	}

	stats2, err := svc.GetStats(2)
	if err != nil {
		t.Fatalf("GetStats user 2: %v", err)
	}
	if stats2.TotalTokens != 200 {
		t.Errorf("user 2 total_tokens = %d, want 200", stats2.TotalTokens)
	}
}

func TestQueryScopesByUser(t *testing.T) {
	svc := newTestUsageService(t)
	u1 := int64(1)
	u2 := int64(2)

	insertUsageLog(t, svc, &u1, 10, 0)
	insertUsageLog(t, svc, &u2, 20, 0)
	insertUsageLog(t, svc, nil, 30, 0)

	logs, total, err := svc.Query(models.UsageQueryParams{}, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if logs[0].TotalTokens != 10 {
		t.Errorf("log tokens = %d, want 10", logs[0].TotalTokens)
	}
}

func TestGetStatsReturnsDBErrors(t *testing.T) {
	svc := newTestUsageService(t)
	if err := svc.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if _, err := svc.GetStats(1); err == nil {
		t.Fatal("expected error from closed database")
	}
}

func TestQueryRejectsForeignAPIKey(t *testing.T) {
	svc := newTestUsageService(t)
	keySvc := NewAPIKeyService(svc.db)

	own, err := keySvc.Create(models.CreateAPIKeyRequest{Name: "mine"}, 1)
	if err != nil {
		t.Fatalf("create key user 1: %v", err)
	}
	other, err := keySvc.Create(models.CreateAPIKeyRequest{Name: "theirs"}, 2)
	if err != nil {
		t.Fatalf("create key user 2: %v", err)
	}

	u1 := int64(1)
	insertUsageLogWithKey(t, svc, &u1, own.APIKey.ID, 10)
	insertUsageLogWithKey(t, svc, &u1, other.APIKey.ID, 20)

	foreignID := other.APIKey.ID
	logs, total, err := svc.Query(models.UsageQueryParams{APIKeyID: &foreignID}, 1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 0 || len(logs) != 0 {
		t.Errorf("foreign api_key_id: total=%d len=%d, want 0", total, len(logs))
	}

	ownID := own.APIKey.ID
	logs, total, err = svc.Query(models.UsageQueryParams{APIKeyID: &ownID}, 1)
	if err != nil {
		t.Fatalf("Query own key: %v", err)
	}
	if total != 1 || len(logs) != 1 || logs[0].TotalTokens != 10 {
		t.Errorf("own api_key_id: total=%d len=%d tokens=%v, want 1/1/10", total, len(logs), logs)
	}
}

func TestGetStatsIncludesActiveKeysAndModels(t *testing.T) {
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	usageSvc := NewUsageService(db)
	keySvc := NewAPIKeyService(db)
	modelSvc := NewModelService(db)

	if _, err := keySvc.Create(models.CreateAPIKeyRequest{Name: "k1"}, 1); err != nil {
		t.Fatalf("create key: %v", err)
	}
	if _, err := keySvc.Create(models.CreateAPIKeyRequest{Name: "k2"}, 1); err != nil {
		t.Fatalf("create key: %v", err)
	}

	pID := seedProvider(t, db, "p1", int64Ptr(1))
	if _, err := db.Exec(
		`INSERT INTO models (provider_id, model_id, provider_key, is_manual, user_id) VALUES (?, 'm1', 'p1', 1, 1)`,
		pID,
	); err != nil {
		t.Fatalf("insert model: %v", err)
	}

	u1 := int64(1)
	insertUsageLog(t, usageSvc, &u1, 5, 0)

	stats, err := usageSvc.GetStats(1)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	stats.ActiveKeys, err = keySvc.CountActive(1)
	if err != nil {
		t.Fatalf("CountActive keys: %v", err)
	}
	stats.ModelsCount, err = modelSvc.CountActive(1)
	if err != nil {
		t.Fatalf("CountActive models: %v", err)
	}

	if stats.ActiveKeys != 2 {
		t.Errorf("active_keys = %d, want 2", stats.ActiveKeys)
	}
	if stats.ModelsCount != 1 {
		t.Errorf("models_count = %d, want 1", stats.ModelsCount)
	}
}

func insertUsageLogWithKey(t *testing.T, svc *UsageService, userID *int64, apiKeyID int64, totalTokens int64) {
	t.Helper()
	_, err := svc.db.Exec(
		`INSERT INTO usage_logs (api_key_id, model, total_tokens, user_id, created_at)
		 VALUES (?, 'test/model', ?, ?, datetime('now'))`,
		apiKeyID, totalTokens, userID,
	)
	if err != nil {
		t.Fatalf("insert usage log: %v", err)
	}
}