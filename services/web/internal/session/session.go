// Package session はアクセストークンを入れる Cookie を扱う。
//
// トークンはブラウザの JS から読めない HttpOnly Cookie にだけ置き、
// web サーバーが Router へ Authorization ヘッダとして転送する。
package session

import "net/http"

const cookieName = "session"

// Set はトークンを Cookie に入れる。Max-Age はトークンの有効期間に揃え、
// 期限切れのトークンをブラウザに残さない。
//
// SameSite=Strict: 他サイトからの遷移・フォーム送信には Cookie を付けない（CSRF 対策の一層目）。
func Set(w http.ResponseWriter, token string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func Get(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// Clear はログアウト。トークン自体は exp まで有効なまま残る
// （失効手段はリフレッシュトークン導入まで無い。design.md §6.1）。
func Clear(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}
