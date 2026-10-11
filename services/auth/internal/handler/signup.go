package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kayawari/graphql-todo/services/auth/internal/db"
	"github.com/kayawari/graphql-todo/services/auth/internal/password"
)

// pgUniqueViolation は PostgreSQL の unique_violation。
const pgUniqueViolation = "23505"

// signup は credentials 行だけを作る。
//
// user_db のプロフィール行はここでは作らない。JIT provisioning により
// user サブグラフへの初回アクセス時に作られる（design.md §6.4）。
// これにより 2 つの DB をまたぐ分散トランザクションが不要になり、
// かつ外部 IdP へ移行してもこの流れが変わらない。
func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest, "malformed JSON body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	if msg := validateCredentials(req.Email, req.Password); msg != "" {
		writeError(w, http.StatusBadRequest, errInvalidRequest, msg)
		return
	}
	if msg := validateDisplayName(req.DisplayName); msg != "" {
		writeError(w, http.StatusBadRequest, errInvalidRequest, msg)
		return
	}

	hash, err := password.Hash(req.Password)
	if err != nil {
		h.internalError(w, "hash password", err)
		return
	}

	cred, err := h.q.CreateCredential(r.Context(), db.CreateCredentialParams{
		Email:        req.Email,
		PasswordHash: hash,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			// REST なので 409 をそのまま表現できる。
			// GraphQL だと 200 + errors 配列になっていた（design.md §9.1）。
			writeError(w, http.StatusConflict, errEmailExists, "")
			return
		}
		h.internalError(w, "create credential", err)
		return
	}

	h.respondWithToken(w, http.StatusCreated, cred.ID, cred.Email, req.DisplayName)
}
