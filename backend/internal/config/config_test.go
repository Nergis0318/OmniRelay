package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Load()
	if cfg.ListenAddr != ":8080" {
		t.Errorf("expected :8080, got %s", cfg.ListenAddr)
	}
	if cfg.DatabasePath != "data/omnirelay.db" {
		t.Errorf("expected data/omnirelay.db, got %s", cfg.DatabasePath)
	}
	if cfg.JWTSecret == "" {
		t.Error("JWTSecret should not be empty")
	}
	if cfg.EncryptKey == "" {
		t.Error("EncryptKey should not be empty")
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("LISTEN_ADDR", ":9090")
	os.Setenv("DATABASE_PATH", "/tmp/test.db")
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("ENCRYPT_KEY", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	os.Setenv("CORS_ORIGINS", "https://example.com,https://app.example.com")
	defer func() {
		os.Unsetenv("LISTEN_ADDR")
		os.Unsetenv("DATABASE_PATH")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ENCRYPT_KEY")
		os.Unsetenv("CORS_ORIGINS")
	}()

	cfg := Load()
	if cfg.ListenAddr != ":9090" {
		t.Errorf("expected :9090, got %s", cfg.ListenAddr)
	}
	if cfg.DatabasePath != "/tmp/test.db" {
		t.Errorf("expected /tmp/test.db, got %s", cfg.DatabasePath)
	}
	if cfg.JWTSecret != "test-secret" {
		t.Errorf("expected test-secret, got %s", cfg.JWTSecret)
	}
	if cfg.EncryptKey != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("unexpected encrypt key: %s", cfg.EncryptKey)
	}
	if cfg.CORSOrigins != "https://example.com,https://app.example.com" {
		t.Errorf("expected cors origins, got %s", cfg.CORSOrigins)
	}
}

func TestPassthroughDefaults(t *testing.T) {
	cfg := Load()
	if !cfg.PassthroughEnabled {
		t.Error("passthrough should default to enabled")
	}
	if cfg.PassthroughAllowPrivate {
		t.Error("private targets should be blocked by default")
	}
	if cfg.PassthroughTimeout <= 0 {
		t.Errorf("passthrough timeout = %v, want a positive duration", cfg.PassthroughTimeout)
	}
}

func TestPassthroughFromEnv(t *testing.T) {
	os.Setenv("PASSTHROUGH_ENABLED", "false")
	os.Setenv("PASSTHROUGH_ALLOW_PRIVATE", "1")
	os.Setenv("PASSTHROUGH_TIMEOUT_SECONDS", "45")
	defer func() {
		os.Unsetenv("PASSTHROUGH_ENABLED")
		os.Unsetenv("PASSTHROUGH_ALLOW_PRIVATE")
		os.Unsetenv("PASSTHROUGH_TIMEOUT_SECONDS")
	}()

	cfg := Load()
	if cfg.PassthroughEnabled {
		t.Error("PASSTHROUGH_ENABLED=false should disable the relay")
	}
	if !cfg.PassthroughAllowPrivate {
		t.Error("PASSTHROUGH_ALLOW_PRIVATE=1 should allow private targets")
	}
	if cfg.PassthroughTimeout != 45*time.Second {
		t.Errorf("timeout = %v, want 45s", cfg.PassthroughTimeout)
	}
}

func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_EXISTS", "hello")
	defer os.Unsetenv("TEST_EXISTS")

	if got := getEnv("TEST_EXISTS", "default"); got != "hello" {
		t.Errorf("expected hello, got %s", got)
	}
	if got := getEnv("TEST_MISSING", "default"); got != "default" {
		t.Errorf("expected default, got %s", got)
	}
}
