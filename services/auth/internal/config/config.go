// Package config は環境変数からの設定読み込みをまとめる。
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string

	JWTIssuer      string
	JWTAudience    string
	AccessTokenTTL time.Duration
	PrivateKeyPath string
}

func Load() (*Config, error) {
	ttl, err := time.ParseDuration(env("ACCESS_TOKEN_TTL", "15m"))
	if err != nil {
		return nil, fmt.Errorf("ACCESS_TOKEN_TTL: %w", err)
	}

	c := &Config{
		ListenAddr:     env("LISTEN_ADDR", ":8083"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTIssuer:      env("JWT_ISSUER", "http://auth:8083"),
		JWTAudience:    env("JWT_AUDIENCE", "graphql-todo-api"),
		AccessTokenTTL: ttl,
		PrivateKeyPath: env("JWT_PRIVATE_KEY_PATH", "/run/secrets/jwt_private.pem"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
