// Package service は todo サブグラフのドメインロジックを持つ。
//
// このパッケージのメソッドは必ず userID を引数に取る。「所有者を指定しない取得」を
// 型レベルで書けなくすることで、認可漏れの余地を消す（design.md §6.7-1）。
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kayawari/graphql-todo/services/todo/internal/db"
)

var ErrNotFound = errors.New("todo not found")

type TodoService struct {
	q *db.Queries
}

func New(q *db.Queries) *TodoService {
	return &TodoService{q: q}
}

func (s *TodoService) Create(ctx context.Context, p db.CreateTodoParams) (db.Todo, error) {
	return s.q.CreateTodo(ctx, p)
}

func (s *TodoService) Get(ctx context.Context, id, userID uuid.UUID) (db.Todo, error) {
	t, err := s.q.GetTodo(ctx, db.GetTodoParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		// 「存在しない」と「他人のもの」を区別しない（design.md §6.8-#5）
		return db.Todo{}, ErrNotFound
	}
	return t, err
}

func (s *TodoService) List(ctx context.Context, p db.ListTodosParams) ([]db.Todo, error) {
	return s.q.ListTodos(ctx, p)
}

// Update は WHERE id = $n AND user_id = $m で絞る。
// 「取得 → 所有者チェック → 更新」にしないのは、チェックと更新の間に
// 競合が入る余地を消すため（design.md §6.7-1）。
func (s *TodoService) Update(ctx context.Context, p db.UpdateTodoParams) (db.Todo, error) {
	t, err := s.q.UpdateTodo(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Todo{}, ErrNotFound
	}
	return t, err
}

func (s *TodoService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	rows, err := s.q.DeleteTodo(ctx, db.DeleteTodoParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
