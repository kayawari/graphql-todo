// web: html/template でサーバー描画するフロントエンド（BFF）。
//
// GraphQL グラフには参加しない。ブラウザの Cookie に入れたアクセストークンを
// サーバー側で Router へ転送するだけで、認可の判断はバックエンドに任せる。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kayawari/graphql-todo/services/web/internal/backend"
	"github.com/kayawari/graphql-todo/services/web/internal/config"
	"github.com/kayawari/graphql-todo/services/web/internal/handler"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hc := &http.Client{Timeout: 10 * time.Second}
	h, err := handler.New(
		backend.NewAuth(cfg.AuthURL, hc),
		backend.NewGraphQL(cfg.GraphQLURL, hc),
		cfg.PageSize,
		cfg.CookieSecure,
		log,
	)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("web listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
