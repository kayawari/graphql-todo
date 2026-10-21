// Package handler は web の画面とルーティングを持つ。
package handler

import (
	"context"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/kayawari/graphql-todo/services/web/internal/backend"
	"github.com/kayawari/graphql-todo/services/web/internal/session"
	"github.com/kayawari/graphql-todo/services/web/web"
)

type Handler struct {
	auth         *backend.Auth
	gql          *backend.GraphQL
	pages        map[string]*template.Template
	pageSize     int
	cookieSecure bool
	log          *slog.Logger
}

func New(auth *backend.Auth, gql *backend.GraphQL, pageSize int, cookieSecure bool, log *slog.Logger) (*Handler, error) {
	pages := map[string]*template.Template{}
	// 各ページは layout + 自身（+ 一覧なら items 断片）で 1 セットにする。
	// content を各ページで定義し直すため、ページごとに別の Template が要る。
	for name, files := range map[string][]string{
		"login":  {"login.html"},
		"signup": {"signup.html"},
		"list":   {"list.html", "items.html"},
		"edit":   {"edit.html"},
		"error":  {"error.html"},
	} {
		patterns := []string{"templates/layout.html"}
		for _, f := range files {
			patterns = append(patterns, "templates/"+f)
		}
		t, err := template.ParseFS(web.Templates, patterns...)
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}

	return &Handler{
		auth:         auth,
		gql:          gql,
		pages:        pages,
		pageSize:     pageSize,
		cookieSecure: cookieSecure,
		log:          log,
	}, nil
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /login", h.loginForm)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("GET /signup", h.signupForm)
	mux.HandleFunc("POST /signup", h.signup)
	mux.HandleFunc("POST /logout", h.logout)

	mux.Handle("GET /{$}", h.requireSession(h.list))
	mux.Handle("GET /todos/page", h.requireSession(h.listPage))
	mux.Handle("POST /todos", h.requireSession(h.create))
	mux.Handle("GET /todos/{id}/edit", h.requireSession(h.editForm))
	mux.Handle("POST /todos/{id}", h.requireSession(h.update))
	mux.Handle("POST /todos/{id}/done", h.requireSession(h.done))
	mux.Handle("POST /todos/{id}/delete", h.requireSession(h.delete))

	static, _ := fs.Sub(web.Static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	return sameOrigin(mux)
}

// creds はリクエスト中のトークン。query でリフレッシュしたら書き換えるのでポインタで持つ。
type creds struct {
	access    string
	refresh   string
	refreshed bool // 1 リクエストでリフレッシュは 1 回まで
}

type credsKey struct{}

func credsFrom(ctx context.Context) *creds {
	c, _ := ctx.Value(credsKey{}).(*creds)
	return c
}

// requireSession は Cookie の無いリクエストを /login へ送る。
//
// アクセストークンの Cookie が切れていてリフレッシュトークンがあれば、ここで出し直す
// （Cookie の Max-Age はトークンの exp に揃えてあるので、通常の失効はこの経路になる）。
// トークンの正当性はここでは確かめない。Router が検証し、駄目なら 401 を返すので
// それを query で受ける（検証箇所を増やさない）。
func (h *Handler) requireSession(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ログアウト後に「戻る」でキャッシュ済みの一覧が見えないようにする。
		w.Header().Set("Cache-Control", "no-store")

		c := &creds{access: session.Get(r), refresh: session.GetRefresh(r)}
		if c.access == "" {
			if c.refresh == "" {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			if err := h.refresh(w, r, c); err != nil {
				h.fail(w, r, "refresh", err)
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), credsKey{}, c)))
	})
}

// query は Router にクエリを投げる。401 なら一度だけリフレッシュして投げ直す。
//
// mutation を投げ直しても二重実行にはならない。Router の 401 は
// require_authentication による拒否で、サブグラフに届く前に返っているため。
func (h *Handler) query(w http.ResponseWriter, r *http.Request, q string, vars map[string]any, out any) error {
	c := credsFrom(r.Context())
	err := h.gql.Do(r.Context(), c.access, q, vars, out)
	if !errors.Is(err, backend.ErrUnauthenticated) || c.refresh == "" || c.refreshed {
		return err
	}
	if err := h.refresh(w, r, c); err != nil {
		return err
	}
	return h.gql.Do(r.Context(), c.access, q, vars, out)
}

// refresh は auth でトークンを出し直し、Cookie と c を更新する。
// レスポンス本体を書く前に呼ぶこと（Set-Cookie を出すため）。
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request, c *creds) error {
	c.refreshed = true
	tok, err := h.auth.Refresh(r.Context(), c.refresh)
	if err != nil {
		return err
	}
	h.saveTokens(w, tok)
	c.access = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refresh = tok.RefreshToken
	}
	return nil
}

// saveTokens は auth の応答を Cookie に入れる。RefreshToken が空（並行リクエストの
// 猶予内。design.md §6.9）なら、リフレッシュトークンの Cookie は触らない。
func (h *Handler) saveTokens(w http.ResponseWriter, tok *backend.Token) {
	session.Set(w, tok.AccessToken, tok.ExpiresIn, h.cookieSecure)
	if tok.RefreshToken != "" {
		session.SetRefresh(w, tok.RefreshToken, tok.RefreshExpiresIn, h.cookieSecure)
	}
}

// sameOrigin は他オリジンからの POST を拒否する（CSRF 対策の二層目。一層目は SameSite=Strict）。
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			src := r.Header.Get("Origin")
			if src == "" {
				src = r.Header.Get("Referer")
			}
			u, err := url.Parse(src)
			if src == "" || err != nil || u.Host != r.Host {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// page はテンプレートに渡す値。画面ごとに使うフィールドだけ埋める。
type page struct {
	Title string
	User  string
	Error string
	Form  map[string]string
	Items []todoView
	Next  string
	Todo  *todoView
}

func (h *Handler) render(w http.ResponseWriter, status int, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
		h.log.Error("render", "page", name, "err", err)
	}
}

// fail はバックエンドのエラーを画面に変換する。
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, backend.ErrUnauthenticated), errors.Is(err, backend.ErrSessionExpired):
		// リフレッシュしても通らない（リフレッシュトークンの期限切れ・失効、鍵ローテーション）。
		// Cookie を捨ててログインからやり直す。
		session.Clear(w, h.cookieSecure)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	case errors.Is(err, backend.ErrNotFound):
		// 他人の TODO もここに来る。「存在しない」と区別しない（design.md §6.7-2）。
		h.render(w, http.StatusNotFound, "error", page{Title: "見つかりません", Error: "TODO が見つかりません。"})
	default:
		h.log.Error("backend", "op", op, "err", err)
		h.render(w, http.StatusInternalServerError, "error", page{Title: "エラー", Error: "エラーが発生しました。時間をおいて再度お試しください。"})
	}
}
