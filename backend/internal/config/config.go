package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

const (
	defaultJWTSecret  = "omnirelay-dev-secret-change-me"
	defaultEncryptKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	defaultPassthroughTimeout = 15 * time.Minute
)

type Config struct {
	ListenAddr   string
	DatabasePath string
	JWTSecret    string
	EncryptKey   string
	CORSOrigins  string

	// PassthroughEnabled serves /https://host/... relays.
	PassthroughEnabled bool
	// PassthroughAllowPrivate lifts the SSRF guard so local model servers
	// (Ollama, LM Studio) can be reached through the relay.
	PassthroughAllowPrivate bool
	// PassthroughTimeout caps one relayed exchange, streamed body included.
	PassthroughTimeout time.Duration
}

func Load() *Config {
	cfg := &Config{
		ListenAddr:              getEnv("LISTEN_ADDR", ":8080"),
		DatabasePath:            getEnv("DATABASE_PATH", "data/omnirelay.db"),
		JWTSecret:               getEnv("JWT_SECRET", defaultJWTSecret),
		EncryptKey:              getEnv("ENCRYPT_KEY", defaultEncryptKey),
		CORSOrigins:             getEnv("CORS_ORIGINS", ""),
		PassthroughEnabled:      getEnvBool("PASSTHROUGH_ENABLED", true),
		PassthroughAllowPrivate: getEnvBool("PASSTHROUGH_ALLOW_PRIVATE", false),
		PassthroughTimeout:      getEnvDuration("PASSTHROUGH_TIMEOUT_SECONDS", defaultPassthroughTimeout),
	}

	if os.Getenv("GIN_MODE") == "release" {
		if cfg.JWTSecret == defaultJWTSecret {
			log.Fatal("FATAL: JWT_SECRET must be set in production (GIN_MODE=release)")
		}
		if cfg.EncryptKey == defaultEncryptKey {
			log.Fatal("FATAL: ENCRYPT_KEY must be set in production (GIN_MODE=release)")
		}
	} else {
		if cfg.JWTSecret == defaultJWTSecret || cfg.EncryptKey == defaultEncryptKey {
			log.Println("WARNING: using default secrets — set JWT_SECRET and ENCRYPT_KEY for production")
		}
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvBool reads a permissive boolean (1/true/yes/on, case-insensitive).
func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		log.Printf("WARNING: %s=%q is not a boolean, using %v", key, val, defaultVal)
		return defaultVal
	}
	return parsed
}

// getEnvDuration reads a whole-number seconds value.
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	seconds, err := strconv.Atoi(val)
	if err != nil || seconds <= 0 {
		log.Printf("WARNING: %s=%q is not a positive number of seconds, using %s", key, val, defaultVal)
		return defaultVal
	}
	return time.Duration(seconds) * time.Second
}
