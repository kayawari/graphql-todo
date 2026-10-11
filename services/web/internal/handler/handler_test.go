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

// fakeRouter は Router の代わり。受け取ったクエリを記録し、respond の結果を返す。
type fakeRouter struct {
	queries []string
	respond func(w http.ResponseWriter, query string)
}

func (f *fakeRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.queries = append(f.queries, body.Query)
	f.respond(w, body.Query)
}

func newTestServer(t *testing.T, router *fakeRouter) *httptest.Server {
	t.Helper()
	rs := httptest.NewServer(router)
	t.Cleanup(rs.Close)

	h, err := New(
		backend.NewAuth("http://auth.invalid", rs.Client()),
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
	if withSession {
		req.AddCookie(&http.Cookie{Name: "session", Value: "tok"})
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
