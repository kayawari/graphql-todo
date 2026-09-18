// auth サービス: GraphQL グラフに参加しない REST サービス（ADR 0001）。
//
// グラフの外に置くことで Router の require_authentication: true が使え、
// 「@authenticated の付け忘れ = 漏洩」が「付け忘れ = 動かない」に変わる。
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kayawari/jikken/graphql-todo/services/auth/internal/config"
	"github.com/kayawari/jikken/graphql-todo/services/auth/internal/db"
	"github.com/kayawari/jikken/graphql-todo/services/auth/internal/handler"
	"github.com/kayawari/jikken/graphql-todo/services/auth/internal/issuer"
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

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return err
	}

	iss, err := issuer.New(cfg.PrivateKeyPath, cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTokenTTL)
	if err != nil {
		return err
	}
	// kid をログに出しておくと、鍵ローテーションの前後を追える（design.md §6.2）。
	log.Info("signing key loaded", "kid", iss.KeyID(), "ttl", cfg.AccessTokenTTL.String())

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler.New(db.New(pool), iss, log),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("auth service listening", "addr", cfg.ListenAddr)
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
