package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kayawari/graphql-todo/services/auth/internal/refresh"
)

// refresh はリフレッシュトークンを使ってアクセストークンを出し直す（design.md §6.9）。
//
// 不明・期限切れ・再利用のどれも同じ 401 にする。理由を区別すると
// 「この値は正しい形だが使用済み」のような手がかりを渡してしまう（design.md §6.8-#9）。
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, errInvalidRequest, "refreshToken is required")
		return
	}

	res, err := h.rt.Rotate(r.Context(), req.RefreshToken)
	if err != nil {
		var reuse *refresh.ReuseError
		switch {
		case errors.As(err, &reuse):
			// 盗用の疑い。運用側が追えるように warn で残す（トークンの値は出さない）。
			h.log.Warn("refresh token reuse detected", "user_id", reuse.UserID, "family_id", reuse.FamilyID)
			writeError(w, http.StatusUnauthorized, errInvalidGrant, "")
		case errors.Is(err, refresh.ErrInvalid):
			writeError(w, http.StatusUnauthorized, errInvalidGrant, "")
		default:
			h.internalError(w, "rotate refresh token", err)
		}
		return
	}

	// display_name は login と同じく空にする。JIT provisioning はサインアップ直後に
	// 済んでいるはずで、ここで claim に載せる必要がない。
	h.respondWithToken(w, http.StatusOK, res.UserID, res.Email, "", res.New)
}
