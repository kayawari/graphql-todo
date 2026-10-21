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

	// リフレッシュトークン（design.md §6.9）。time.ParseDuration は d を受け付けないので時間で書く。
	RefreshTokenTTL    time.Duration // 使うたびに延びる期限
	RefreshTokenMaxAge time.Duration // ログインからの絶対上限
	RefreshReuseGrace  time.Duration // 並行リクエストを再利用とみなさない猶予
}

func Load() (*Config, error) {
	ttl, err := duration("ACCESS_TOKEN_TTL", "15m")
	if err != nil {
		return nil, err
	}
	refreshTTL, err := duration("REFRESH_TOKEN_TTL", "336h") // 14 日
	if err != nil {
		return nil, err
	}
	refreshMaxAge, err := duration("REFRESH_TOKEN_MAX_AGE", "720h") // 30 日
	if err != nil {
		return nil, err
	}
	reuseGrace, err := duration("REFRESH_REUSE_GRACE", "10s")
	if err != nil {
		return nil, err
	}

	c := &Config{
		ListenAddr:     env("LISTEN_ADDR", ":8083"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTIssuer:      env("JWT_ISSUER", "http://auth:8083"),
		JWTAudience:    env("JWT_AUDIENCE", "graphql-todo-api"),
		AccessTokenTTL: ttl,
		PrivateKeyPath: env("JWT_PRIVATE_KEY_PATH", "/run/secrets/jwt_private.pem"),

		RefreshTokenTTL:    refreshTTL,
		RefreshTokenMaxAge: refreshMaxAge,
		RefreshReuseGrace:  reuseGrace,
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func duration(key, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(env(key, fallback))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
