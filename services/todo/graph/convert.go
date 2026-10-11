package graph

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kayawari/graphql-todo/services/todo/graph/model"
	"github.com/kayawari/graphql-todo/services/todo/internal/db"
)

func toTodoModel(t db.Todo) *model.Todo {
	m := &model.Todo{
		ID:          t.ID.String(),
		Title:       t.Title,
		Description: t.Description,
		Status:      model.TodoStatus(t.Status),
		CreatedAt:   t.CreatedAt.Time,
		UpdatedAt:   t.UpdatedAt.Time,
		// 所有者は ID だけ埋める。残りのフィールド（displayName など）は
		// Router が user サブグラフへ問い合わせて解決する。
		// これが Federation の entity 解決そのもの。
		User: &model.User{ID: t.UserID.String()},
	}
	if t.DueDate.Valid {
		d := t.DueDate.Time
		m.DueDate = &d
	}
	return m
}

// toTodoModels は必ず非 nil のスライスを返す。
//
// sqlc の :many は 0 行のとき nil スライスを返し、nil スライスは JSON で
// null にシリアライズされる。スキーマは [Todo!]!（non-null）なので、
// 正規化しないとバリデーションエラーになる（design.md §6.8-#7）。
func toTodoModels(ts []db.Todo) []*model.Todo {
	out := make([]*model.Todo, 0, len(ts))
	for _, t := range ts {
		out = append(out, toTodoModel(t))
	}
	return out
}

func toDBStatus(s *model.TodoStatus) *db.TodoStatus {
	if s == nil {
		return nil
	}
	v := db.TodoStatus(*s)
	return &v
}

func toDBDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}
