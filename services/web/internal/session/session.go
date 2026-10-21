// Package session はアクセストークンとリフレッシュトークンを入れる Cookie を扱う。
//
// トークンはブラウザの JS から読めない HttpOnly Cookie にだけ置き、
// web サーバーが Router へ Authorization ヘッダとして転送する。
package session

import "net/http"

const (
	accessCookie  = "session"
	refreshCookie = "refresh"
)

// Set はアクセストークンを Cookie に入れる。Max-Age はトークンの有効期間に揃え、
// 期限切れのトークンをブラウザに残さない。Cookie が消えたら、次のリクエストで
// リフレッシュトークンから出し直す（design.md §4.7）。
func Set(w http.ResponseWriter, token string, maxAge int, secure bool) {
	set(w, accessCookie, token, maxAge, secure)
}

// Get はアクセストークンを返す。無ければ空文字。
func Get(r *http.Request) string { return get(r, accessCookie) }

// SetRefresh はリフレッシュトークンを Cookie に入れる（design.md §6.9）。
//
// Path は / のまま。どの画面でもアクセストークンの出し直しが起こり得るため。
func SetRefresh(w http.ResponseWriter, token string, maxAge int, secure bool) {
	set(w, refreshCookie, token, maxAge, secure)
}

// GetRefresh はリフレッシュトークンを返す。無ければ空文字。
func GetRefresh(r *http.Request) string { return get(r, refreshCookie) }

// Clear は両方の Cookie を消す。サーバー側の失効（auth の /auth/logout）は呼び出し側で行う。
func Clear(w http.ResponseWriter, secure bool) {
	set(w, accessCookie, "", -1, secure)
	set(w, refreshCookie, "", -1, secure)
}

// SameSite=Strict: 他サイトからの遷移・フォーム送信には Cookie を付けない（CSRF 対策の一層目）。
func set(w http.ResponseWriter, name, value string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func get(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
