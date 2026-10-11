// todo サブグラフ: Todo エンティティを所有し、User.todos を拡張する。
//
// 認証は auth サービス（グラフ外）が行い、ここでは JWT の再検証だけを行う
// （design.md §6.6）。公開オペレーションは 1 つも無い。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/kayawari/graphql-todo/services/todo/graph"
	"github.com/kayawari/graphql-todo/services/todo/graph/generated"
	"github.com/kayawari/graphql-todo/services/todo/internal/auth"
	"github.com/kayawari/graphql-todo/services/todo/internal/config"
	"github.com/kayawari/graphql-todo/services/todo/internal/db"
	"github.com/kayawari/graphql-todo/services/todo/internal/service"
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

	// JWKS は起動時に取りにいかない。auth サービスの起動を待たずに
	// 立ち上がれるよう、初回検証時に遅延取得する（design.md §8.2）。
	verifier := auth.NewVerifier(ctx, cfg.JWKSURL, cfg.JWTIssuer, cfg.JWTAudience, log)

	resolver := &graph.Resolver{
		Service: service.New(db.New(pool)),
		Log:     log,
	}

	gql := handler.New(generated.NewExecutableSchema(generated.Config{Resolvers: resolver}))
	gql.AddTransport(transport.Options{})
	gql.AddTransport(transport.GET{})
	gql.AddTransport(transport.POST{})
	gql.SetQueryCache(lru.New[*ast.QueryDocument](256))
	// wgc router compose がスキーマを introspection するため必須（design.md §8.3）。
	gql.Use(extension.Introspection{})

	mux := http.NewServeMux()
	mux.Handle("/graphql", verifier.Middleware(gql))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("todo subgraph listening", "addr", cfg.ListenAddr)
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
