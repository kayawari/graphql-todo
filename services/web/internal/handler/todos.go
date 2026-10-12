package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/kayawari/graphql-todo/services/web/internal/backend"
)

// todoView はテンプレート用。dueDate は <input type="date"> と同じ YYYY-MM-DD にする。
type todoView struct {
	ID          string
	Title       string
	Description string
	DueDate     string
}

type todoNode struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	DueDate     *string `json:"dueDate"`
}

func (n todoNode) view() todoView {
	v := todoView{ID: n.ID, Title: n.Title}
	if n.Description != nil {
		v.Description = *n.Description
	}
	if n.DueDate != nil {
		if t, err := time.Parse(time.RFC3339, *n.DueDate); err == nil {
			v.DueDate = t.Format(time.DateOnly)
		}
	}
	return v
}

// 一覧は未完了（TODO）だけ。DONE は表示しない。
const listQuery = `query($first: Int!, $after: String) {
  me { displayName }
  todoConnection(filter: { status: TODO }, first: $first, after: $after) {
    edges { node { id title description dueDate } }
    pageInfo { hasNextPage endCursor }
  }
}`

const pageQuery = `query($first: Int!, $after: String) {
  todoConnection(filter: { status: TODO }, first: $first, after: $after) {
    edges { node { id title description dueDate } }
    pageInfo { hasNextPage endCursor }
  }
}`

type connection struct {
	Edges []struct {
		Node todoNode `json:"node"`
	} `json:"edges"`
	PageInfo struct {
		HasNextPage bool    `json:"hasNextPage"`
		EndCursor   *string `json:"endCursor"`
	} `json:"pageInfo"`
}

func (c connection) items() ([]todoView, string) {
	items := make([]todoView, 0, len(c.Edges))
	for _, e := range c.Edges {
		items = append(items, e.Node.view())
	}
	next := ""
	if c.PageInfo.HasNextPage && c.PageInfo.EndCursor != nil {
		next = *c.PageInfo.EndCursor
	}
	return items, next
}

func (h *Handler) pageVars(r *http.Request) map[string]any {
	vars := map[string]any{"first": h.pageSize}
	if after := r.URL.Query().Get("after"); after != "" {
		vars["after"] = after
	}
	return vars
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Me struct {
			DisplayName string `json:"displayName"`
		} `json:"me"`
		TodoConnection connection `json:"todoConnection"`
	}
	if err := h.gql.Do(r.Context(), tokenFrom(r.Context()), listQuery, h.pageVars(r), &data); err != nil {
		h.fail(w, r, "list", err)
		return
	}
	items, next := data.TodoConnection.items()
	h.render(w, http.StatusOK, "list", page{
		Title: "一覧",
		User:  data.Me.DisplayName,
		Items: items,
		Next:  next,
	})
}

// listPage は Infinite スクロール用の HTML 断片（<li> の並び）だけを返す。
func (h *Handler) listPage(w http.ResponseWriter, r *http.Request) {
	var data struct {
		TodoConnection connection `json:"todoConnection"`
	}
	if err := h.gql.Do(r.Context(), tokenFrom(r.Context()), pageQuery, h.pageVars(r), &data); err != nil {
		h.fail(w, r, "listPage", err)
		return
	}
	items, next := data.TodoConnection.items()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.pages["list"].ExecuteTemplate(w, "items", page{Items: items, Next: next}); err != nil {
		h.log.Error("render", "page", "items", "err", err)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	f, err := parseTodoForm(r)
	if err != nil {
		h.badRequest(w, err.Error())
		return
	}
	const q = `mutation($input: CreateTodoInput!) { createTodo(input: $input) { id } }`
	err = h.gql.Do(r.Context(), tokenFrom(r.Context()), q, map[string]any{"input": f.createInput()}, nil)
	if h.inputError(w, err) {
		return
	}
	if err != nil {
		h.fail(w, r, "create", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	const q = `query($id: ID!) {
  me { displayName }
  todo(id: $id) { id title description dueDate }
}`
	var data struct {
		Me struct {
			DisplayName string `json:"displayName"`
		} `json:"me"`
		Todo *todoNode `json:"todo"`
	}
	if err := h.gql.Do(r.Context(), tokenFrom(r.Context()), q, map[string]any{"id": r.PathValue("id")}, &data); err != nil {
		h.fail(w, r, "edit", err)
		return
	}
	if data.Todo == nil {
		h.fail(w, r, "edit", backend.ErrNotFound)
		return
	}
	v := data.Todo.view()
	h.render(w, http.StatusOK, "edit", page{Title: "編集", User: data.Me.DisplayName, Todo: &v})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	f, err := parseTodoForm(r)
	if err != nil {
		h.badRequest(w, err.Error())
		return
	}
	h.mutate(w, r, "update",
		`mutation($id: ID!, $input: UpdateTodoInput!) { updateTodo(id: $id, input: $input) { id } }`,
		map[string]any{"id": r.PathValue("id"), "input": f.updateInput()})
}

func (h *Handler) done(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "done",
		`mutation($id: ID!) { updateTodo(id: $id, input: { status: DONE }) { id } }`,
		map[string]any{"id": r.PathValue("id")})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "delete",
		`mutation($id: ID!) { deleteTodo(id: $id) }`,
		map[string]any{"id": r.PathValue("id")})
}

func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, op, q string, vars map[string]any) {
	err := h.gql.Do(r.Context(), tokenFrom(r.Context()), q, vars, nil)
	if h.inputError(w, err) {
		return
	}
	if err != nil {
		h.fail(w, r, op, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// inputError は BAD_USER_INPUT を 400 のエラー画面にする。処理したら true。
func (h *Handler) inputError(w http.ResponseWriter, err error) bool {
	var ie *backend.InputError
	if !errors.As(err, &ie) {
		return false
	}
	h.badRequest(w, ie.Msg)
	return true
}

func (h *Handler) badRequest(w http.ResponseWriter, msg string) {
	h.render(w, http.StatusBadRequest, "error", page{Title: "入力エラー", Error: msg})
}
