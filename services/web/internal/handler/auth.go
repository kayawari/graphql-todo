package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/kayawari/graphql-todo/services/web/internal/backend"
	"github.com/kayawari/graphql-todo/services/web/internal/session"
)

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if session.Get(r) != "" || session.GetRefresh(r) != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, http.StatusOK, "login", page{Title: "ログイン"})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	tok, err := h.auth.Login(r.Context(), email, r.FormValue("password"))
	if err != nil {
		p := page{Title: "ログイン", Form: map[string]string{"Email": email}}
		var ie *backend.InputError
		switch {
		case errors.Is(err, backend.ErrInvalidCredentials):
			// 失敗理由は区別しない（design.md §6.8-#9）
			p.Error = "メールアドレスまたはパスワードが正しくありません。"
			h.render(w, http.StatusUnauthorized, "login", p)
		case errors.As(err, &ie):
			p.Error = ie.Msg
			h.render(w, http.StatusBadRequest, "login", p)
		default:
			h.fail(w, r, "login", err)
		}
		return
	}
	h.saveTokens(w, tok)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) signupForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, "signup", page{Title: "サインアップ"})
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	name := strings.TrimSpace(r.FormValue("displayName"))
	tok, err := h.auth.Signup(r.Context(), email, r.FormValue("password"), name)
	if err != nil {
		p := page{Title: "サインアップ", Form: map[string]string{"Email": email, "DisplayName": name}}
		var ie *backend.InputError
		switch {
		case errors.Is(err, backend.ErrEmailExists):
			p.Error = "このメールアドレスは登録済みです。"
			h.render(w, http.StatusConflict, "signup", p)
		case errors.As(err, &ie):
			p.Error = ie.Msg
			h.render(w, http.StatusBadRequest, "signup", p)
		default:
			h.fail(w, r, "signup", err)
		}
		return
	}
	h.saveTokens(w, tok)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout はリフレッシュトークンを auth 側で失効させてから Cookie を消す。
// アクセストークンは最大 15 分で自然に切れる（design.md §6.1）。
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if rt := session.GetRefresh(r); rt != "" {
		// 失効に失敗しても、利用者から見たログアウトは止めない。
		if err := h.auth.Logout(r.Context(), rt); err != nil {
			h.log.Warn("revoke refresh token", "err", err)
		}
	}
	session.Clear(w, h.cookieSecure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
