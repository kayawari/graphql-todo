package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kayawari/graphql-todo/services/web/internal/backend"
)

// fakeRouter は Router の代わり。受け取ったクエリとトークンを記録し、respond の結果を返す。
type fakeRouter struct {
	queries []string
	tokens  []string
	respond func(w http.ResponseWriter, query string)
}

func (f *fakeRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.queries = append(f.queries, body.Query)
	f.tokens = append(f.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	f.respond(w, body.Query)
}

// fakeAuth は auth サービスの代わり。呼ばれたパスと受け取った refreshToken を記録する。
type fakeAuth struct {
	calls   []string
	respond func(w http.ResponseWriter, path, refreshToken string)
}

func (f *fakeAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refreshToken"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.calls = append(f.calls, r.URL.Path+" "+body.RefreshToken)
	f.respond(w, r.URL.Path, body.RefreshToken)
}

func newTestServer(t *testing.T, router *fakeRouter) *httptest.Server {
	t.Helper()
	return newTestServerWithAuth(t, router, &fakeAuth{respond: func(http.ResponseWriter, string, string) {
		t.Error("auth must not be called")
	}})
}

func newTestServerWithAuth(t *testing.T, router *fakeRouter, auth *fakeAuth) *httptest.Server {
	t.Helper()
	rs := httptest.NewServer(router)
	t.Cleanup(rs.Close)
	as := httptest.NewServer(auth)
	t.Cleanup(as.Close)

	h, err := New(
		backend.NewAuth(as.URL, as.Client()),
		backend.NewGraphQL(rs.URL, rs.Client()),
		20, false, slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	ws := httptest.NewServer(h.Routes())
	t.Cleanup(ws.Close)
	return ws
}

// noRedirect はリダイレクトを追わずにレスポンスを返すクライアント。
var noRedirect = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func do(t *testing.T, ws *httptest.Server, method, path string, withSession bool, form url.Values) *http.Response {
	t.Helper()
	var cookies []*http.Cookie
	if withSession {
		cookies = append(cookies, &http.Cookie{Name: "session", Value: "tok"})
	}
	return doWithCookies(t, ws, method, path, form, cookies...)
}

func doWithCookies(t *testing.T, ws *httptest.Server, method, path string, form url.Values, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, ws.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", ws.URL)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

const emptyList = `{"data":{"me":{"displayName":"A"},"todoConnection":{"edges":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`

func TestRequiresSession(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { t.Error("router must not be called") }}
	ws := newTestServer(t, router)

	for _, c := range []struct{ method, path string }{
		{"GET", "/"},
		{"GET", "/todos/page"},
		{"GET", "/todos/x/edit"},
		{"POST", "/todos"},
		{"POST", "/todos/x/done"},
		{"POST", "/todos/x/delete"},
	} {
		res := do(t, ws, c.method, c.path, false, url.Values{})
		if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
			t.Errorf("%s %s: got %d %q, want 303 /login", c.method, c.path, res.StatusCode, res.Header.Get("Location"))
		}
	}
}

func TestListShowsOnlyTodoStatusAndIsNotCached(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { io.WriteString(w, emptyList) }}
	ws := newTestServer(t, router)

	res := do(t, ws, "GET", "/", true, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if len(router.queries) != 1 || !strings.Contains(router.queries[0], "status: TODO") {
		t.Errorf("list query must filter status: TODO, got %q", router.queries)
	}
}

func TestRouter401ClearsSession(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { w.WriteHeader(http.StatusUnauthorized) }}
	ws := newTestServer(t, router)

	res := do(t, ws, "GET", "/", true, nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 303 /login", res.StatusCode, res.Header.Get("Location"))
	}
	cleared := false
	for _, c := range res.Cookies() {
		if c.Name == "session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("session cookie was not cleared")
	}
}

func TestNotFoundFromRouter(t *testing.T) {
	// Router がサブグラフのエラーを extensions.errors に包んだ形（実物と同じ）
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) {
		io.WriteString(w, `{"errors":[{"message":"Failed to fetch from Subgraph 'todo'.","extensions":{"errors":[{"message":"not found","extensions":{"code":"NOT_FOUND"}}]}}],"data":{"me":{"displayName":"A"},"todo":null}}`)
	}}
	ws := newTestServer(t, router)

	if res := do(t, ws, "GET", "/todos/someone-elses/edit", true, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("edit: status %d, want 404", res.StatusCode)
	}
	if res := do(t, ws, "POST", "/todos/someone-elses/delete", true, url.Values{}); res.StatusCode != http.StatusNotFound {
		t.Errorf("delete: status %d, want 404", res.StatusCode)
	}
}

func TestCrossOriginPostIsRejected(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { t.Error("router must not be called") }}
	ws := newTestServer(t, router)

	for _, origin := range []string{"http://evil.example", ""} {
		req, _ := http.NewRequest("POST", ws.URL+"/todos/x/delete", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.AddCookie(&http.Cookie{Name: "session", Value: "tok"})
		res, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("origin %q: status %d, want 403", origin, res.StatusCode)
		}
	}
}

func TestListPageReturnsFragmentWithNextLink(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) {
		io.WriteString(w, `{"data":{"todoConnection":{"edges":[{"node":{"id":"1","title":"<b>x</b>","description":null,"dueDate":null}}],"pageInfo":{"hasNextPage":true,"endCursor":"CUR"}}}}`)
	}}
	ws := newTestServer(t, router)

	res := do(t, ws, "GET", "/todos/page?after=PREV", true, nil)
	b, _ := io.ReadAll(res.Body)
	html := string(b)
	if strings.Contains(html, "<html") {
		t.Error("fragment must not include the layout")
	}
	if !strings.Contains(html, "&lt;b&gt;x&lt;/b&gt;") {
		t.Error("title must be HTML-escaped")
	}
	if !strings.Contains(html, `data-page="/todos/page?after=CUR"`) {
		t.Errorf("next link missing: %s", html)
	}
}

// cookie はレスポンスの Set-Cookie から name のものを探す。
func cookie(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

const refreshed = `{"accessToken":"new-access","expiresIn":900,"refreshToken":"new-refresh","refreshExpiresIn":1209600}`

// アクセストークンの Cookie が切れていても、リフレッシュトークンがあれば出し直して続行する。
func TestMissingAccessTokenIsRefreshed(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { io.WriteString(w, emptyList) }}
	auth := &fakeAuth{respond: func(w http.ResponseWriter, _, _ string) { io.WriteString(w, refreshed) }}
	ws := newTestServerWithAuth(t, router, auth)

	res := doWithCookies(t, ws, "GET", "/", nil, &http.Cookie{Name: "refresh", Value: "old-refresh"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
	if want := []string{"/auth/refresh old-refresh"}; strings.Join(auth.calls, ",") != strings.Join(want, ",") {
		t.Errorf("auth calls = %q, want %q", auth.calls, want)
	}
	if len(router.tokens) != 1 || router.tokens[0] != "new-access" {
		t.Errorf("router tokens = %q, want [new-access]", router.tokens)
	}
	if c := cookie(res, "session"); c == nil || c.Value != "new-access" || c.MaxAge != 900 {
		t.Errorf("session cookie = %v, want new-access / 900", c)
	}
	if c := cookie(res, "refresh"); c == nil || c.Value != "new-refresh" || !c.HttpOnly {
		t.Errorf("refresh cookie = %v, want new-refresh (HttpOnly)", c)
	}
}

// Router が 401 を返したら、一度だけリフレッシュして投げ直す。
func TestRouter401IsRetriedAfterRefresh(t *testing.T) {
	router := &fakeRouter{}
	router.respond = func(w http.ResponseWriter, _ string) {
		if router.tokens[len(router.tokens)-1] != "new-access" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"data":{"createTodo":{"id":"1"}}}`)
	}
	auth := &fakeAuth{respond: func(w http.ResponseWriter, _, _ string) { io.WriteString(w, refreshed) }}
	ws := newTestServerWithAuth(t, router, auth)

	res := doWithCookies(t, ws, "POST", "/todos", url.Values{"title": {"x"}},
		&http.Cookie{Name: "session", Value: "expired"},
		&http.Cookie{Name: "refresh", Value: "old-refresh"})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/" {
		t.Fatalf("got %d %q, want 303 /", res.StatusCode, res.Header.Get("Location"))
	}
	if want := "expired,new-access"; strings.Join(router.tokens, ",") != want {
		t.Errorf("router tokens = %q, want %q", router.tokens, want)
	}
	if len(auth.calls) != 1 {
		t.Errorf("auth calls = %q, want 1 refresh", auth.calls)
	}
}

// 猶予内の応答（refreshToken なし）では、リフレッシュトークンの Cookie を触らない。
func TestAccessOnlyRefreshKeepsRefreshCookie(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { io.WriteString(w, emptyList) }}
	auth := &fakeAuth{respond: func(w http.ResponseWriter, _, _ string) {
		io.WriteString(w, `{"accessToken":"new-access","expiresIn":900}`)
	}}
	ws := newTestServerWithAuth(t, router, auth)

	res := doWithCookies(t, ws, "GET", "/", nil, &http.Cookie{Name: "refresh", Value: "old-refresh"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
	if c := cookie(res, "refresh"); c != nil {
		t.Errorf("refresh cookie must not be touched, got %v", c)
	}
}

// リフレッシュも拒否されたら、両方の Cookie を消してログインへ戻す。Router は呼ばない。
func TestRefreshRejectedClearsBothCookies(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { t.Error("router must not be called") }}
	auth := &fakeAuth{respond: func(w http.ResponseWriter, _, _ string) { w.WriteHeader(http.StatusUnauthorized) }}
	ws := newTestServerWithAuth(t, router, auth)

	res := doWithCookies(t, ws, "GET", "/", nil, &http.Cookie{Name: "refresh", Value: "revoked"})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 303 /login", res.StatusCode, res.Header.Get("Location"))
	}
	for _, name := range []string{"session", "refresh"} {
		if c := cookie(res, name); c == nil || c.MaxAge >= 0 {
			t.Errorf("%s cookie = %v, want cleared", name, c)
		}
	}
}

// ログアウトは auth でファミリーを失効させ、両方の Cookie を消す。
func TestLogoutRevokesRefreshToken(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { t.Error("router must not be called") }}
	auth := &fakeAuth{respond: func(w http.ResponseWriter, _, _ string) { w.WriteHeader(http.StatusNoContent) }}
	ws := newTestServerWithAuth(t, router, auth)

	res := doWithCookies(t, ws, "POST", "/logout", url.Values{},
		&http.Cookie{Name: "session", Value: "tok"},
		&http.Cookie{Name: "refresh", Value: "my-refresh"})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Fatalf("got %d %q, want 303 /login", res.StatusCode, res.Header.Get("Location"))
	}
	if want := "/auth/logout my-refresh"; strings.Join(auth.calls, ",") != want {
		t.Errorf("auth calls = %q, want %q", auth.calls, want)
	}
	for _, name := range []string{"session", "refresh"} {
		if c := cookie(res, name); c == nil || c.MaxAge >= 0 {
			t.Errorf("%s cookie = %v, want cleared", name, c)
		}
	}
}
