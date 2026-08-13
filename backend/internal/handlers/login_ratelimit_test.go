package handlers

import (
	"testing"
	"time"
)

func TestLoginRateLimiterBlocksAfterMaxAttempts(t *testing.T) {
	limiter := newLoginRateLimiter(3, time.Hour, 5)

	if !limiter.allow("10.0.0.1") || !limiter.allow("10.0.0.1") || !limiter.allow("10.0.0.1") {
		t.Fatal("first three attempts should be allowed")
	}
	if limiter.allow("10.0.0.1") {
		t.Fatal("fourth attempt should be blocked")
	}
}

func TestLoginRateLimiterTracksIPsIndependently(t *testing.T) {
	limiter := newLoginRateLimiter(3, time.Hour, 5)

	if !limiter.allow("10.0.0.1") || !limiter.allow("10.0.0.1") || !limiter.allow("10.0.0.1") {
		t.Fatal("first three attempts from IP1 should be allowed")
	}
	if limiter.allow("10.0.0.1") {
		t.Fatal("IP1 should be blocked")
	}
	if !limiter.allow("10.0.0.2") {
		t.Fatal("IP2 should still be allowed")
	}
}

func TestLoginRateLimiterResetOnSuccess(t *testing.T) {
	limiter := newLoginRateLimiter(3, time.Hour, 5)

	limiter.allow("10.0.0.1")
	limiter.allow("10.0.0.1")
	limiter.reset("10.0.0.1")
	if !limiter.allow("10.0.0.1") || !limiter.allow("10.0.0.1") || !limiter.allow("10.0.0.1") {
		t.Fatal("attempts should reset after successful login")
	}
}

func TestLoginRateLimiterWindowExpires(t *testing.T) {
	limiter := newLoginRateLimiter(2, time.Millisecond, 5)

	limiter.allow("10.0.0.1")
	limiter.allow("10.0.0.1")
	if limiter.allow("10.0.0.1") {
		t.Fatal("third attempt should be blocked within window")
	}

	time.Sleep(5 * time.Millisecond)
	if !limiter.allow("10.0.0.1") {
		t.Fatal("attempts after window expiry should be allowed")
	}
}

func TestLoginRateLimiterEvictsOldestEntries(t *testing.T) {
	limiter := newLoginRateLimiter(1, time.Hour, 2)

	limiter.allow("10.0.0.1")
	limiter.allow("10.0.0.2")
	limiter.allow("10.0.0.3")
	// maxIPs=2 evicted 10.0.0.1, so it should be allowed again.
	if !limiter.allow("10.0.0.1") {
		t.Fatal("evicted IP should be allowed again")
	}
}

func TestClientIPExtraction(t *testing.T) {
	cases := []struct {
		remoteAddr    string
		xForwardedFor string
		want          string
	}{
		{"10.0.0.1:1234", "", "10.0.0.1"},
		{"10.0.0.1:1234", "1.2.3.4", "1.2.3.4"},
		{"10.0.0.1:1234", "1.2.3.4, 5.6.7.8", "1.2.3.4"},
		{"", "1.2.3.4", "1.2.3.4"},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := clientIPForRateLimit(tc.remoteAddr, tc.xForwardedFor); got != tc.want {
			t.Errorf("clientIPForRateLimit(%q, %q) = %q, want %q", tc.remoteAddr, tc.xForwardedFor, got, tc.want)
		}
	}
}
