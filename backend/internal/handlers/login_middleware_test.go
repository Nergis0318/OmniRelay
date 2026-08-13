package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"omnirelay/internal/database"
	"omnirelay/internal/models"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

func testAuthDB(t *testing.T) (*sql.DB, error) {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { db.Close() })
	return db, nil
}

func registerReq(username, email, password string) models.RegisterRequest {
	return models.RegisterRequest{
		Username: username,
		Email:    email,
		Password: password,
	}
}

// TestLoginRateLimitMiddlewareBlocksAfterLimit drives the middleware through
// a real gin context to confirm 429 responses appear after the attempt cap.
func TestLoginRateLimitMiddlewareBlocksAfterLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := loginLimiter
	loginLimiter = newLoginRateLimiter(3, loginWindow, 100)
	defer func() { loginLimiter = orig }()

	r := gin.New()
	r.POST("/admin/auth/login", LoginRateLimit(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"token": "t"})
	})

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", strings.NewReader(`{}`))
		req.RemoteAddr = "10.0.0.1:1234"
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/login", strings.NewReader(`{}`))
	req.RemoteAddr = "10.0.0.1:1234"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
}

// TestLoginResetsRateLimitOnSuccess confirms a successful login through the
// handler clears the limiter for the client IP.
func TestLoginResetsRateLimitOnSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := loginLimiter
	loginLimiter = newLoginRateLimiter(2, loginWindow, 100)
	defer func() { loginLimiter = orig }()

	db, err := testAuthDB(t)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	svc := service.NewAuthService(db)
	svc.SetJWTSecret("test-secret")

	if _, err := svc.Register(registerReq("alice", "alice@example.com", "password1")); err != nil {
		t.Fatalf("register: %v", err)
	}

	r := gin.New()
	r.POST("/login", LoginRateLimit(), Login(svc))

	login := func(password string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		body := `{"email":"alice@example.com","password":"` + password + `"}`
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		req.RemoteAddr = "10.0.0.7:1234"
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	if w := login("wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status = %d, want 401", w.Code)
	}
	// Second attempt: at limit, but a successful login resets the counter.
	if w := login("password1"); w.Code != http.StatusOK {
		t.Fatalf("correct password: status = %d, want 200", w.Code)
	}
	if w := login("wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("after reset, another attempt allowed: status = %d, want 401", w.Code)
	}
}
