// Package handler は auth サービスの HTTP エンドポイントを提供する。
//
// GraphQL ではなく REST なのは、トークン発行に GraphQL の強み（フィールド選択、
// entity 解決）が要らないうえ、Set-Cookie・429・WWW-Authenticate といった
// HTTP 層の関心事と素直に噛み合うため（ADR 0001 §1.3）。
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/kayawari/graphql-todo/services/auth/internal/db"
	"github.com/kayawari/graphql-todo/services/auth/internal/issuer"
)

type Handler struct {
	q   *db.Queries
	iss *issuer.Issuer
	log *slog.Logger
}

func New(q *db.Queries, iss *issuer.Issuer, log *slog.Logger) http.Handler {
	h := &Handler{q: q, iss: iss, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/signup", h.signup)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("GET /.well-known/jwks.json", h.jwks)
	mux.HandleFunc("GET /health", h.health)
	return mux
}

// ---- リクエスト / レスポンス（design.md §4.1）----

type signupRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken string `json:"accessToken"`
	TokenType   string `json:"tokenType"`
	ExpiresIn   int    `json:"expiresIn"`
	UserID      string `json:"userId"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// ---- エラー表現（design.md §9.1）----

const (
	errInvalidRequest     = "invalid_request"
	errInvalidCredentials = "invalid_credentials"
	errEmailExists        = "email_already_exists"
	errInternal           = "internal_error"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{Error: code, Message: msg})
}

// internalError は詳細をログにだけ残し、クライアントには何も伝えない
// （design.md §9.2 の ErrorPresenter と同じ方針）。
func (h *Handler) internalError(w http.ResponseWriter, op string, err error) {
	h.log.Error("internal error", "op", op, "err", err)
	writeError(w, http.StatusInternalServerError, errInternal, "")
}

// ---- バリデーション ----

func validateCredentials(email, pw string) string {
	if _, err := mail.ParseAddress(email); err != nil {
		return "email must be a valid address"
	}
	if len(pw) < 8 {
		return "password must be at least 8 characters"
	}
	return ""
}

func validateDisplayName(name string) string {
	n := len([]rune(strings.TrimSpace(name)))
	if n < 1 || n > 50 {
		return "displayName must be between 1 and 50 characters"
	}
	return ""
}

func (h *Handler) respondWithToken(w http.ResponseWriter, status int, userID uuid.UUID, email, displayName string) {
	tok, err := h.iss.Issue(userID, email, displayName)
	if err != nil {
		h.internalError(w, "issue token", err)
		return
	}
	writeJSON(w, status, tokenResponse{
		AccessToken: tok,
		TokenType:   "Bearer",
		ExpiresIn:   int(h.iss.TTL().Seconds()),
		UserID:      userID.String(),
	})
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// jwks は公開鍵を配信する。認証不要（公開鍵しか含まないため）。
//
// ただし本番では必ず TLS か VPC 内部通信にする。取得経路を中間者に
// 差し替えられると攻撃者が自前の鍵でトークンを偽造できてしまう。
// 守るべきは機密性ではなく完全性（design.md §6.2 セキュリティ注意 1）。
func (h *Handler) jwks(w http.ResponseWriter, _ *http.Request) {
	body, err := h.iss.JWKS()
	if err != nil {
		h.internalError(w, "marshal jwks", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write(body)
}
