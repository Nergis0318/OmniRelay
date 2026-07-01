package config

import "os"

type Config struct {
	ListenAddr   string
	DatabasePath string
	JWTSecret    string
	EncryptKey   string
	CORSOrigins  string
}

func Load() *Config {
	return &Config{
		ListenAddr:   getEnv("LISTEN_ADDR", ":8080"),
		DatabasePath: getEnv("DATABASE_PATH", "data/omnirelay.db"),
		JWTSecret:    getEnv("JWT_SECRET", "omnirelay-dev-secret-change-me"),
		EncryptKey:   getEnv("ENCRYPT_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
		CORSOrigins:  getEnv("CORS_ORIGINS", ""),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
