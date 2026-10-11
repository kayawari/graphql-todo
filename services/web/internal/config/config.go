// Package config は環境変数からの設定読み込みをまとめる。
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ListenAddr string

	// web はグラフに参加しない BFF。auth の REST と Router の GraphQL を
	// サーバー側から呼ぶ。
	AuthURL    string
	GraphQLURL string

	// 本番（HTTPS）では true にする。開発環境は平文 HTTP なので false。
	CookieSecure bool
	PageSize     int
}

func Load() (*Config, error) {
	secure, err := strconv.ParseBool(env("COOKIE_SECURE", "false"))
	if err != nil {
		return nil, fmt.Errorf("COOKIE_SECURE: %w", err)
	}
	size, err := strconv.Atoi(env("PAGE_SIZE", "20"))
	if err != nil || size < 1 || size > 100 {
		return nil, fmt.Errorf("PAGE_SIZE must be between 1 and 100")
	}

	return &Config{
		ListenAddr:   env("LISTEN_ADDR", ":8084"),
		AuthURL:      env("AUTH_URL", "http://auth:8083"),
		GraphQLURL:   env("GRAPHQL_URL", "http://router:3002/graphql"),
		CookieSecure: secure,
		PageSize:     size,
	}, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
