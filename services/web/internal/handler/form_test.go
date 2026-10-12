package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestParseDate(t *testing.T) {
	d, err := ParseDate("2026-10-12")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.String(); got != "2026-10-12" {
		t.Errorf("String() = %q", got)
	}
	b, _ := json.Marshal(d)
	if got := string(b); got != `"2026-10-12T00:00:00Z"` {
		t.Errorf("MarshalJSON = %s", got)
	}

	for _, s := range []string{
		"2026/10/12",
		"2026-1-2",
		"2026-02-30", // 存在しない日付
		"2026-13-01",
		" 2026-10-12",
		"2026-10-12T00:00:00Z",
		"tomorrow",
	} {
		if _, err := ParseDate(s); err == nil {
			t.Errorf("ParseDate(%q): want error", s)
		}
	}
}

func TestInputJSON(t *testing.T) {
	d, _ := ParseDate("2026-10-12")

	// 作成: 空のメモと期限は送らない
	b, _ := json.Marshal(todoForm{Title: "a"}.createInput())
	if got := string(b); got != `{"title":"a"}` {
		t.Errorf("create without optional fields = %s", got)
	}
	b, _ = json.Marshal(todoForm{Title: "a", Description: "m", DueDate: &d}.createInput())
	if got := string(b); got != `{"title":"a","description":"m","dueDate":"2026-10-12T00:00:00Z"}` {
		t.Errorf("create = %s", got)
	}

	// 更新: メモは空でも送る（空文字で上書きする）
	b, _ = json.Marshal(todoForm{Title: "a"}.updateInput())
	if got := string(b); got != `{"title":"a","description":""}` {
		t.Errorf("update = %s", got)
	}
}

func TestInvalidFormIsBadRequest(t *testing.T) {
	router := &fakeRouter{respond: func(w http.ResponseWriter, _ string) { t.Error("router must not be called") }}
	ws := newTestServer(t, router)

	for _, c := range []struct {
		path string
		form url.Values
		want string
	}{
		{"/todos", url.Values{"title": {"a"}, "dueDate": {"2026/10/12"}}, "YYYY-MM-DD"},
		{"/todos/x", url.Values{"title": {"a"}, "dueDate": {"2026-02-30"}}, "YYYY-MM-DD"},
		{"/todos", url.Values{"title": {"  "}}, "タイトル"},
	} {
		res := do(t, ws, "POST", c.path, true, c.form)
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(b), c.want) {
			t.Errorf("POST %s %v: got %d, want 400 containing %q", c.path, c.form, res.StatusCode, c.want)
		}
	}
}
