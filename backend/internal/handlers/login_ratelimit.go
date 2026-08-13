package handlers

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// loginRateLimiter is a per-IP fixed-window limiter used to slow down
// brute-force attacks against /admin/auth/login and /admin/auth/register.
type loginRateLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	maxIPs      int
	attempts    map[string][]time.Time
}

func newLoginRateLimiter(maxAttempts int, window time.Duration, maxIPs int) *loginRateLimiter {
	return &loginRateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		maxIPs:      maxIPs,
		attempts:    make(map[string][]time.Time),
	}
}

// allow reports whether the IP may attempt a login now.
func (l *loginRateLimiter) allow(ip string) bool {
	if ip == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)
	recent := l.attempts[ip][:0]
	for _, t := range l.attempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	l.attempts[ip] = recent

	if len(recent) >= l.maxAttempts {
		return false
	}
	l.attempts[ip] = append(recent, now)
	l.evictIfNeededLocked(ip)
	return true
}

// reset clears the attempt history after a successful login.
func (l *loginRateLimiter) reset(ip string) {
	if ip == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

// evictIfNeededLocked drops the entry whose most recent attempt is oldest
// when the table grows past maxIPs so an attacker cannot exhaust memory with
// spoofed addresses. Callers must hold l.mu.
func (l *loginRateLimiter) evictIfNeededLocked(justAdded string) {
	if len(l.attempts) <= l.maxIPs {
		return
	}
	var oldestKey string
	var oldestTime time.Time
	for ip, times := range l.attempts {
		if ip == justAdded {
			continue
		}
		if len(times) > 0 && (oldestKey == "" || times[0].Before(oldestTime)) {
			oldestKey = ip
			oldestTime = times[0]
		}
	}
	if oldestKey != "" {
		delete(l.attempts, oldestKey)
	}
}

// clientIPForRateLimit resolves the client IP for rate limiting, honoring
// X-Forwarded-For when present (the proxy sits behind Caddy).
func clientIPForRateLimit(remoteAddr, xForwardedFor string) string {
	if xForwardedFor != "" {
		if idx := strings.IndexByte(xForwardedFor, ','); idx >= 0 {
			xForwardedFor = xForwardedFor[:idx]
		}
		if ip := strings.TrimSpace(xForwardedFor); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

const (
	loginMaxAttemptsPerWindow = 10
	loginWindow               = 10 * time.Minute
	loginMaxTrackedIPs        = 10000
)

var loginLimiter = newLoginRateLimiter(loginMaxAttemptsPerWindow, loginWindow, loginMaxTrackedIPs)

// LoginRateLimit rejects further attempts when an IP has made too many
// failed logins recently.
func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := clientIPForRateLimit(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"))
		if !loginLimiter.allow(ip) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts, try again later"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// resetLoginRateLimit clears the attempt history after a successful login.
func resetLoginRateLimit(c *gin.Context) {
	ip := clientIPForRateLimit(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"))
	loginLimiter.reset(ip)
}
