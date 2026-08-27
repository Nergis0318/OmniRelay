package service

import (
	"database/sql"
	"log"
	"omnirelay/internal/models"
	"strconv"
	"sync"
	"time"
)

// passthroughLogBuffer bounds how many relay records may queue up while the
// writer goroutine is busy. Relay requests never wait on SQLite.
const passthroughLogBuffer = 4096

// perfRowLimit caps the rows pulled for percentile maths, mirroring
// PerformanceService.
const perfRowLimit = 50000

type PassthroughService struct {
	db   *sql.DB
	ch   chan models.PassthroughLog
	done chan struct{}
	wg   sync.WaitGroup

	closeOnce sync.Once
	dropped   int64
	dropMu    sync.Mutex
}

func NewPassthroughService(db *sql.DB) *PassthroughService {
	svc := &PassthroughService{
		db:   db,
		ch:   make(chan models.PassthroughLog, passthroughLogBuffer),
		done: make(chan struct{}),
	}
	svc.wg.Add(1)
	go svc.run()
	return svc
}

// Log queues a measured relay for writing without blocking the caller. When the
// queue is full the record is dropped and counted, because accurate timings are
// worth more than a stalled proxy.
func (s *PassthroughService) Log(rec models.PassthroughLog) {
	select {
	case s.ch <- rec:
	default:
		s.dropMu.Lock()
		s.dropped++
		if s.dropped == 1 || s.dropped%100 == 0 {
			log.Printf("passthrough log queue full, dropped %d records", s.dropped)
		}
		s.dropMu.Unlock()
	}
}

func (s *PassthroughService) Dropped() int64 {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	return s.dropped
}

func (s *PassthroughService) run() {
	defer s.wg.Done()
	for {
		select {
		case rec := <-s.ch:
			s.insert(rec)
		case <-s.done:
			for {
				select {
				case rec := <-s.ch:
					s.insert(rec)
				default:
					return
				}
			}
		}
	}
}

// Close drains any queued records and stops the writer.
func (s *PassthroughService) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
	})
	return nil
}

func (s *PassthroughService) insert(rec models.PassthroughLog) {
	_, err := s.db.Exec(
		`INSERT INTO passthrough_logs (host, path, method, status_code, is_error, error_message, dns_ms, connect_ms, tls_ms, ttfb_ms, ttft_ms, total_ms, request_bytes, response_bytes, started_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.Host, rec.Path, rec.Method, rec.StatusCode, boolToInt(rec.IsError), rec.ErrMessage,
		rec.DNSMs, rec.ConnectMs, rec.TLSMs, rec.TTFBMs, rec.TTFTMs, rec.TotalMs,
		rec.RequestBytes, rec.ResponseBytes, utcStamp(rec.StartedAt), utcStamp(time.Now()),
	)
	if err != nil {
		log.Printf("failed to write passthrough log: %v", err)
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// utcStamp stores millisecond-precision timestamps as NULL when absent, so
// aggregates never mistake "no timestamp" for the epoch.
func utcStamp(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	stamp := t.UTC().Format("2006-01-02 15:04:05.000")
	return &stamp
}

func (s *PassthroughService) buildWhere(params models.PassthroughQueryParams) (string, []interface{}) {
	clauses := []string{"1 = 1"}
	args := []interface{}{}
	if params.Host != "" {
		clauses = append(clauses, "host = ?")
		args = append(args, params.Host)
	}
	if params.From != "" {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, params.From)
	}
	if params.To != "" {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, params.To)
	}
	where := ""
	for i, c := range clauses {
		if i > 0 {
			where += " AND "
		}
		where += c
	}
	return where, args
}

func (s *PassthroughService) GetPerformance(params models.PassthroughQueryParams) (*models.PassthroughPerformanceResponse, error) {
	where, args := s.buildWhere(params)

	summary, err := s.querySummary(where, args)
	if err != nil {
		return nil, err
	}
	granularity := resolveGranularity(params.Granularity, params.From, params.To)
	timeseries, err := s.queryTimeseries(where, args, granularity)
	if err != nil {
		return nil, err
	}
	byHost, err := s.queryByHost(where, args)
	if err != nil {
		return nil, err
	}

	return &models.PassthroughPerformanceResponse{
		Summary:    *summary,
		Timeseries: timeseries,
		ByHost:     byHost,
	}, nil
}

func (s *PassthroughService) querySummary(where string, args []interface{}) (*models.PassthroughSummary, error) {
	summary := &models.PassthroughSummary{}

	var (
		errorCount       int64
		avgTotal         sql.NullFloat64
		avgTTFB          sql.NullFloat64
		avgTTFT          sql.NullFloat64
		avgDNS           sql.NullFloat64
		avgConnect       sql.NullFloat64
		avgTLS           sql.NullFloat64
		avgResponseBytes sql.NullFloat64
	)
	err := s.db.QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(is_error),0),
		        COALESCE(AVG(CASE WHEN is_error = 0 THEN total_ms END),0),
		        AVG(ttfb_ms),
		        AVG(ttft_ms),
		        AVG(dns_ms),
		        AVG(connect_ms),
		        AVG(tls_ms),
		        COALESCE(AVG(response_bytes),0)
		 FROM passthrough_logs WHERE `+where,
		args...,
	).Scan(&summary.TotalRequests, &errorCount, &avgTotal, &avgTTFB, &avgTTFT, &avgDNS, &avgConnect, &avgTLS, &avgResponseBytes)
	if err != nil {
		return nil, err
	}

	avg := func(v sql.NullFloat64) *float64 {
		if !v.Valid {
			return nil
		}
		out := v.Float64
		return &out
	}
	summary.AvgTotalMs = avgTotal.Float64
	summary.AvgTTFBMs = avg(avgTTFB)
	summary.AvgTTFTMs = avg(avgTTFT)
	summary.AvgDNSMs = avg(avgDNS)
	summary.AvgConnectMs = avg(avgConnect)
	summary.AvgTLSMs = avg(avgTLS)
	summary.AvgResponseBytes = avgResponseBytes.Float64
	if summary.TotalRequests > 0 {
		summary.ErrorRate = float64(errorCount) / float64(summary.TotalRequests)
	}
	summary.RequestsPerSec = s.requestsPerSecond(where, args, summary.TotalRequests)

	p50, p95, p99, err := s.queryPercentiles(where, args)
	if err != nil {
		return nil, err
	}
	summary.P50TotalMs, summary.P95TotalMs, summary.P99TotalMs = p50, p95, p99
	return summary, nil
}

// requestsPerSecond uses the span actually covered by the stored records, so an
// empty or single-row window reports 0 rather than a wild spike.
func (s *PassthroughService) requestsPerSecond(where string, args []interface{}, total int64) float64 {
	if total <= 0 {
		return 0
	}
	var first, last string
	err := s.db.QueryRow(`SELECT COALESCE(MIN(created_at),''), COALESCE(MAX(created_at),'') FROM passthrough_logs WHERE `+where, args...).Scan(&first, &last)
	if err != nil || first == "" || last == "" {
		return 0
	}
	tFirst, err := parseAnyTime(first)
	if err != nil {
		return 0
	}
	tLast, err := parseAnyTime(last)
	if err != nil {
		return 0
	}
	seconds := tLast.Sub(tFirst).Seconds()
	if seconds < 1 {
		// Timestamps are second-granular; fall back to the started_at stamps,
		// which carry milliseconds.
		var firstStart, lastStart string
		if qerr := s.db.QueryRow(`SELECT COALESCE(MIN(started_at),''), COALESCE(MAX(started_at),'') FROM passthrough_logs WHERE `+where, args...).Scan(&firstStart, &lastStart); qerr == nil && firstStart != "" && lastStart != "" {
			if a, aerr := parseAnyTime(firstStart); aerr == nil {
				if b, berr := parseAnyTime(lastStart); berr == nil {
					seconds = b.Sub(a).Seconds()
				}
			}
		}
	}
	if seconds < 1 {
		return float64(total)
	}
	return float64(total) / seconds
}

func (s *PassthroughService) queryPercentiles(where string, args []interface{}) (float64, float64, float64, error) {
	rows, err := s.db.Query(`SELECT total_ms FROM passthrough_logs WHERE `+where+` AND is_error = 0 ORDER BY total_ms ASC LIMIT `+strconv.Itoa(perfRowLimit), args...)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	sorted := make([]float64, 0, 256)
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return 0, 0, 0, err
		}
		sorted = append(sorted, float64(v))
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	if len(sorted) == 0 {
		return 0, 0, 0, nil
	}
	return percentile(sorted, 0.50), percentile(sorted, 0.95), percentile(sorted, 0.99), nil
}

func (s *PassthroughService) queryTimeseries(where string, args []interface{}, granularity string) ([]models.PassthroughBucket, error) {
	query := `SELECT strftime('` + bucketFormat(granularity) + `', created_at) AS bucket,
		        COUNT(*),
		        COALESCE(SUM(is_error),0),
		        COALESCE(AVG(CASE WHEN is_error = 0 THEN total_ms END),0),
		        COALESCE(MAX(total_ms),0),
		        AVG(ttfb_ms),
		        COALESCE(AVG(response_bytes),0)
		 FROM passthrough_logs WHERE ` + where + ` GROUP BY bucket ORDER BY bucket ASC LIMIT 500`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := []models.PassthroughBucket{}
	for rows.Next() {
		var b models.PassthroughBucket
		var avgTTFB sql.NullFloat64
		if err := rows.Scan(&b.Bucket, &b.RequestCount, &b.ErrorCount, &b.AvgTotalMs, &b.MaxTotalMs, &avgTTFB, &b.AvgResponseBytes); err != nil {
			return nil, err
		}
		if avgTTFB.Valid {
			v := avgTTFB.Float64
			b.AvgTTFBMs = &v
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// queryByHost groups the aggregate view per upstream host.
func (s *PassthroughService) queryByHost(where string, args []interface{}) ([]models.PassthroughHostStats, error) {
	query := `SELECT host,
		        COUNT(*),
		        COALESCE(SUM(is_error),0),
		        COALESCE(AVG(CASE WHEN is_error = 0 THEN total_ms END),0),
		        AVG(ttfb_ms),
		        AVG(ttft_ms),
		        COALESCE(AVG(response_bytes),0)
		 FROM passthrough_logs WHERE ` + where + ` GROUP BY host ORDER BY COUNT(*) DESC LIMIT 20`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := []models.PassthroughHostStats{}
	for rows.Next() {
		var st models.PassthroughHostStats
		var avgTTFB, avgTTFT sql.NullFloat64
		if err := rows.Scan(&st.Host, &st.Requests, &st.Errors, &st.AvgTotalMs, &avgTTFB, &avgTTFT, &st.AvgResponseBytes); err != nil {
			return nil, err
		}
		if avgTTFB.Valid {
			v := avgTTFB.Float64
			st.AvgTTFBMs = &v
		}
		if avgTTFT.Valid {
			v := avgTTFT.Float64
			st.AvgTTFTMs = &v
		}
		stats = append(stats, st)
	}
	return stats, rows.Err()
}

func (s *PassthroughService) List(params models.PassthroughQueryParams) ([]models.PassthroughLog, int64, error) {
	where, args := s.buildWhere(params)

	limit := params.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM passthrough_logs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT id, host, path, method, status_code, is_error, error_message, dns_ms, connect_ms, tls_ms, ttfb_ms, ttft_ms, total_ms, request_bytes, response_bytes, started_at, created_at
	          FROM passthrough_logs WHERE ` + where + ` ORDER BY id DESC LIMIT ` + strconv.Itoa(limit) + ` OFFSET ` + strconv.Itoa(offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	logs := []models.PassthroughLog{}
	for rows.Next() {
		var l models.PassthroughLog
		var isErr int
		var startedAt, createdAt sql.NullString
		if err := rows.Scan(&l.ID, &l.Host, &l.Path, &l.Method, &l.StatusCode, &isErr, &l.ErrMessage,
			&l.DNSMs, &l.ConnectMs, &l.TLSMs, &l.TTFBMs, &l.TTFTMs, &l.TotalMs,
			&l.RequestBytes, &l.ResponseBytes, &startedAt, &createdAt); err != nil {
			return nil, 0, err
		}
		l.IsError = isErr == 1
		if startedAt.Valid {
			if t, err := parseAnyTime(startedAt.String); err == nil {
				l.StartedAt = t
			}
		}
		if createdAt.Valid {
			if t, err := parseAnyTime(createdAt.String); err == nil {
				l.CreatedAt = &t
			}
		}
		logs = append(logs, l)
	}
	return logs, total, rows.Err()
}
