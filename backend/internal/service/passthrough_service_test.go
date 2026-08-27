package service

import (
	"path/filepath"
	"testing"
	"time"

	"omnirelay/internal/database"
	"omnirelay/internal/models"
)

func newTestPassthroughService(t *testing.T) *PassthroughService {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	svc := NewPassthroughService(db)
	t.Cleanup(func() {
		if err := svc.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
		db.Close()
	})
	return svc
}

func insertPassthroughRow(t *testing.T, svc *PassthroughService, host string, totalMs, ttfbMs int64, isError bool, createdAt string) {
	t.Helper()
	isErr := 0
	if isError {
		isErr = 1
	}
	_, err := svc.db.Exec(
		`INSERT INTO passthrough_logs (host, path, method, status_code, is_error, ttfb_ms, total_ms, response_bytes, created_at)
		 VALUES (?, '/v1/chat/completions', 'POST', ?, ?, ?, ?, 512, ?)`,
		host, 500, isErr, ttfbMs, totalMs, createdAt,
	)
	if err != nil {
		t.Fatalf("insert passthrough row: %v", err)
	}
}

// TestLogWritesAsynchronously covers the contract the relay depends on: Log
// returns immediately and the writer goroutine persists the row, including the
// nullable timing columns.
func TestLogWritesAsynchronously(t *testing.T) {
	svc := newTestPassthroughService(t)

	now := time.Now()
	svc.Log(models.PassthroughLog{
		Host:          "api.openai.com",
		Path:          "/v1/chat/completions",
		Method:        "POST",
		StatusCode:    200,
		TTFBMs:        ptrInt64(120),
		TTFTMs:        ptrInt64(340),
		TotalMs:       900,
		RequestBytes:  64,
		ResponseBytes: 2048,
		StartedAt:     now,
	})
	svc.Log(models.PassthroughLog{Host: "ollama.local", Method: "GET", StatusCode: 502, IsError: true, ErrMessage: "boom", TotalMs: 5})

	if dropped := svc.Dropped(); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	logs, total, err := svc.List(models.PassthroughQueryParams{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	// ORDER BY id DESC, so the newest record comes first.
	newest := logs[0]
	if newest.Host != "ollama.local" || !newest.IsError || newest.ErrMessage != "boom" {
		t.Errorf("newest record = %+v", newest)
	}
	if !newest.StartedAt.IsZero() {
		t.Errorf("started_at = %v, want zero when the relay recorded none", newest.StartedAt)
	}
	older := logs[1]
	if older.Host != "api.openai.com" || older.TotalMs != 900 {
		t.Fatalf("older record = %+v", older)
	}
	if older.StartedAt.IsZero() {
		t.Error("started_at not persisted")
	}
	if older.TTFBMs == nil || *older.TTFBMs != 120 {
		t.Errorf("ttfb_ms = %v, want 120", older.TTFBMs)
	}
	if older.TTFTMs == nil || *older.TTFTMs != 340 {
		t.Errorf("ttft_ms = %v, want 340", older.TTFTMs)
	}
	if older.DNSMs != nil || older.ConnectMs != nil || older.TLSMs != nil {
		t.Errorf("unset timing columns should stay NULL, got %+v", older)
	}
}

func TestPassthroughGetPerformance(t *testing.T) {
	svc := newTestPassthroughService(t)

	day := "2026-08-27 10:00:00"
	for _, total := range []int64{10, 20, 30, 40, 50} {
		insertPassthroughRow(t, svc, "api.openai.com", total, total*2, false, day)
	}
	insertPassthroughRow(t, svc, "api.openai.com", 9999, 9999, true, day)
	insertPassthroughRow(t, svc, "localhost:11434", 15, 30, false, day)
	// A host that only ever failed must report unknown latency, never 0ms.
	insertPassthroughRow(t, svc, "dead.example", 8000, 8000, true, day)

	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	resp, err := svc.GetPerformance(models.PassthroughQueryParams{})
	if err != nil {
		t.Fatalf("GetPerformance: %v", err)
	}

	if resp.Summary.TotalRequests != 8 {
		t.Errorf("total_requests = %d, want 8", resp.Summary.TotalRequests)
	}
	if resp.Summary.ErrorRate < 0.24 || resp.Summary.ErrorRate > 0.26 {
		t.Errorf("error_rate = %v, want ~0.25", resp.Summary.ErrorRate)
	}
	// Errors are excluded from latency averages and percentiles.
	if got := derefFloat(t, resp.Summary.AvgTotalMs); got != 27.5 { // (10+20+30+40+50+15)/6
		t.Errorf("avg_total_ms = %v, want 27.5", got)
	}
	if got := derefFloat(t, resp.Summary.P50TotalMs); got != 20 {
		t.Errorf("p50_total_ms = %v, want 20", got)
	}
	if got := derefFloat(t, resp.Summary.P95TotalMs); got != 50 {
		t.Errorf("p95_total_ms = %v, want 50", got)
	}
	if resp.Summary.AvgTTFBMs == nil {
		t.Error("avg_ttfb_ms is nil, want a value")
	}

	if len(resp.ByHost) != 3 {
		t.Fatalf("by_host = %+v, want 3 hosts", resp.ByHost)
	}
	if resp.ByHost[0].Host != "api.openai.com" || resp.ByHost[0].Requests != 6 {
		t.Errorf("top host = %+v, want api.openai.com with 6 requests", resp.ByHost[0])
	}
	if resp.ByHost[0].Errors != 1 {
		t.Errorf("top host errors = %d, want 1", resp.ByHost[0].Errors)
	}
	for _, h := range resp.ByHost {
		if h.Host == "dead.example" && h.AvgTotalMs != nil {
			t.Errorf("all-error host avg_total_ms = %v, want nil", *h.AvgTotalMs)
		}
	}
	if len(resp.Timeseries) != 1 {
		t.Errorf("timeseries buckets = %d, want 1", len(resp.Timeseries))
	}

	filtered, err := svc.GetPerformance(models.PassthroughQueryParams{Host: "localhost:11434"})
	if err != nil {
		t.Fatalf("GetPerformance(host): %v", err)
	}
	if filtered.Summary.TotalRequests != 1 || derefFloat(t, filtered.ByHost[0].AvgTotalMs) != 15 {
		t.Errorf("host filter = %+v, by_host = %+v", filtered.Summary, filtered.ByHost)
	}
	if filtered.ByHost[0].AvgTTFTMs != nil {
		t.Errorf("avg_ttft_ms = %v, want nil for a non-streamed row", *filtered.ByHost[0].AvgTTFTMs)
	}
}

func TestPassthroughListFiltersByTime(t *testing.T) {
	svc := newTestPassthroughService(t)

	insertPassthroughRow(t, svc, "api.openai.com", 10, 20, false, "2026-08-26 10:00:00")
	insertPassthroughRow(t, svc, "api.openai.com", 20, 30, false, "2026-08-27 10:00:00")
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	logs, total, err := svc.List(models.PassthroughQueryParams{From: "2026-08-27 00:00:00"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("total = %d, len = %d, want 1/1", total, len(logs))
	}
	if logs[0].TotalMs != 20 {
		t.Errorf("total_ms = %d, want 20", logs[0].TotalMs)
	}
}

// TestLogPersistsTokenColumns round-trips the five usage columns through Log ->
// insert -> List, including the all-null case.
func TestLogPersistsTokenColumns(t *testing.T) {
	svc := newTestPassthroughService(t)

	svc.Log(models.PassthroughLog{
		Host: "api.anthropic.com", Method: "POST", StatusCode: 200,
		InputTokens: ptrInt64(120), OutputTokens: ptrInt64(45),
		CacheWrite5MTokens: ptrInt64(88), CacheWrite1HTokens: ptrInt64(7), CacheReadTokens: ptrInt64(12),
	})
	svc.Log(models.PassthroughLog{Host: "api.openai.com", Method: "POST", StatusCode: 200})

	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	logs, total, err := svc.List(models.PassthroughQueryParams{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	var anthropic, plain models.PassthroughLog
	for _, l := range logs {
		switch l.Host {
		case "api.anthropic.com":
			anthropic = l
		case "api.openai.com":
			plain = l
		}
	}
	if anthropic.Host != "api.anthropic.com" || plain.Host != "api.openai.com" {
		t.Fatalf("records not found: %+v", logs)
	}
	if *anthropic.InputTokens != 120 || *anthropic.OutputTokens != 45 {
		t.Errorf("tokens = %v/%v, want 120/45", anthropic.InputTokens, anthropic.OutputTokens)
	}
	if *anthropic.CacheWrite5MTokens != 88 || *anthropic.CacheWrite1HTokens != 7 || *anthropic.CacheReadTokens != 12 {
		t.Errorf("cache = %v/%v/%v, want 88/7/12",
			anthropic.CacheWrite5MTokens, anthropic.CacheWrite1HTokens, anthropic.CacheReadTokens)
	}
	if plain.InputTokens != nil || plain.OutputTokens != nil ||
		plain.CacheWrite5MTokens != nil || plain.CacheWrite1HTokens != nil || plain.CacheReadTokens != nil {
		t.Errorf("usage-less relay must store NULLs, got %+v", plain)
	}
}

func TestPerformanceTokenAggregatesAreNullWithoutUsage(t *testing.T) {
	svc := newTestPassthroughService(t)

	day := "2026-08-27 10:00:00"
	insertPassthroughRow(t, svc, "api.openai.com", 10, 20, false, day)

	usageSvc := svc
	usageSvc.Log(models.PassthroughLog{
		Host: "api.anthropic.com", Method: "POST", StatusCode: 200,
		InputTokens: ptrInt64(100), OutputTokens: ptrInt64(30),
		CacheReadTokens: ptrInt64(50),
	})

	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	resp, err := svc.GetPerformance(models.PassthroughQueryParams{})
	if err != nil {
		t.Fatalf("GetPerformance: %v", err)
	}
	summary := resp.Summary
	if summary.TotalInputTokens == nil || *summary.TotalInputTokens != 100 {
		t.Errorf("total_input_tokens = %v, want 100", summary.TotalInputTokens)
	}
	if summary.TotalOutputTokens == nil || *summary.TotalOutputTokens != 30 {
		t.Errorf("total_output_tokens = %v, want 30", summary.TotalOutputTokens)
	}
	if summary.TotalCacheReadTokens == nil || *summary.TotalCacheReadTokens != 50 {
		t.Errorf("total_cache_read_tokens = %v, want 50", summary.TotalCacheReadTokens)
	}
	// Anthropic never reported a 1h cache write; nothing else did either.
	if summary.TotalCacheWrite1HToks != nil {
		t.Errorf("total_cache_write_1h_tokens = %v, want null", *summary.TotalCacheWrite1HToks)
	}

	for _, h := range resp.ByHost {
		if h.Host == "api.anthropic.com" && (h.OutputTokens == nil || *h.OutputTokens != 30) {
			t.Errorf("by_host output_tokens = %v, want 30 for api.anthropic.com", h.OutputTokens)
		}
		if h.Host == "api.openai.com" && h.OutputTokens != nil {
			t.Errorf("by_host output_tokens = %v, want null for the usage-less host", *h.OutputTokens)
		}
	}

	bucket := resp.Timeseries[0]
	if bucket.InputTokens == nil || *bucket.InputTokens != 100 {
		t.Errorf("bucket input_tokens = %v, want 100", bucket.InputTokens)
	}
}

func ptrInt64(v int64) *int64 { return &v }

func derefFloat(t *testing.T, v *float64) float64 {
	t.Helper()
	if v == nil {
		t.Fatal("expected a value, got nil")
	}
	return *v
}
