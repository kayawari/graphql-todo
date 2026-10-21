package handler

import (
	"encoding/json"
	"net/http"
)

// logout はリフレッシュトークンのファミリーを失効させる（design.md §6.9）。
//
// アクセストークンは最大 15 分で自然に切れるので、ブロックリストは持たない。
// トークンが有効だったかどうかは返さず、常に 204 にする。
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, errInvalidRequest, "refreshToken is required")
		return
	}
	if err := h.rt.Revoke(r.Context(), req.RefreshToken); err != nil {
		h.internalError(w, "revoke refresh token", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
