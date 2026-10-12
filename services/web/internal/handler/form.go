package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// Date は時刻を持たない日付。<input type="date"> が送る YYYY-MM-DD だけを受け付ける。
type Date struct{ t time.Time }

var errInvalidDate = errors.New("期限は YYYY-MM-DD 形式の日付で入力してください")

// ParseDate は YYYY-MM-DD を厳密に解釈する。
// 2026/10/12・2026-1-2・2026-02-30 のように形式や暦が違う値は、黙って捨てずにエラーにする。
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	// 解釈できても書き戻した結果が一致しなければ受け付けない（桁数の違いなどを弾く）
	if err != nil || t.Format(time.DateOnly) != s {
		return Date{}, errInvalidDate
	}
	return Date{t: t}, nil
}

// MarshalJSON は GraphQL の Time スカラー（RFC3339）として送る。
func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.t.Format(time.RFC3339) + `"`), nil
}

func (d Date) String() string { return d.t.Format(time.DateOnly) }

// createTodoInput / updateTodoInput は GraphQL の入力型と同じ形。
// map ではなく struct にして、送るフィールドと型をここで固定する。
// user_id は持たない（design.md §6.7-4）。
type createTodoInput struct {
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	DueDate     *Date   `json:"dueDate,omitempty"`
}

type updateTodoInput struct {
	Title string `json:"title"`
	// 編集画面でメモを空にしたら空文字で上書きする（省略＝既存値維持とは区別する）。
	Description string `json:"description"`
	// 空欄なら省略する。バックエンドは省略を「変更しない」と扱うため、
	// 既存の期限は消せない（todo.md #9）。
	DueDate *Date `json:"dueDate,omitempty"`
}

// todoForm はフォームの値を検証済みの型に変換したもの。
type todoForm struct {
	Title       string
	Description string
	DueDate     *Date
}

// parseTodoForm はフォームを検証する。エラーは利用者にそのまま見せてよい文言で返す。
func parseTodoForm(r *http.Request) (todoForm, error) {
	f := todoForm{
		Title:       strings.TrimSpace(r.FormValue("title")),
		Description: strings.TrimSpace(r.FormValue("description")),
	}
	if f.Title == "" {
		return todoForm{}, errors.New("タイトルを入力してください")
	}
	if s := r.FormValue("dueDate"); s != "" {
		d, err := ParseDate(s)
		if err != nil {
			return todoForm{}, err
		}
		f.DueDate = &d
	}
	return f, nil
}

func (f todoForm) createInput() createTodoInput {
	in := createTodoInput{Title: f.Title, DueDate: f.DueDate}
	if f.Description != "" {
		in.Description = &f.Description
	}
	return in
}

func (f todoForm) updateInput() updateTodoInput {
	return updateTodoInput{Title: f.Title, Description: f.Description, DueDate: f.DueDate}
}
