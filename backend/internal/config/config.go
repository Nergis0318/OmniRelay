package config

import (
	"log"
	"os"
)

const (
	defaultJWTSecret  = "omnirelay-dev-secret-change-me"
	defaultEncryptKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

type Config struct {
	ListenAddr   string
	DatabasePath string
	JWTSecret    string
	EncryptKey   string
	CORSOrigins  string
}

func Load() *Config {
	cfg := &Config{
		ListenAddr:   getEnv("LISTEN_ADDR", ":8080"),
		DatabasePath: getEnv("DATABASE_PATH", "data/omnirelay.db"),
		JWTSecret:    getEnv("JWT_SECRET", defaultJWTSecret),
		EncryptKey:   getEnv("ENCRYPT_KEY", defaultEncryptKey),
		CORSOrigins:  getEnv("CORS_ORIGINS", ""),
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
