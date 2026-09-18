package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string

	JWKSURL     string
	JWTIssuer   string
	JWTAudience string
}

func Load() (*Config, error) {
	c := &Config{
		ListenAddr:  env("LISTEN_ADDR", ":8081"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWKSURL:     env("JWKS_URL", "http://auth:8083/.well-known/jwks.json"),
		JWTIssuer:   env("JWT_ISSUER", "http://auth:8083"),
		JWTAudience: env("JWT_AUDIENCE", "graphql-todo-api"),
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
