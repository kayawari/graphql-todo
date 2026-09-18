-- design.md §6.7-1「sqlc のクエリは user_id を必ず引数に取る」。
-- WHERE id = $1 だけのクエリをこのファイルに書いてはいけない。

-- name: CreateTodo :one
INSERT INTO todos (user_id, title, description, due_date)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetTodo :one
SELECT * FROM todos
WHERE id = $1 AND user_id = $2;

-- name: ListTodos :many
SELECT * FROM todos
WHERE user_id = @user_id
  AND (sqlc.narg('status')::todo_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('due_before')::date IS NULL OR due_date < sqlc.narg('due_before'))
ORDER BY created_at DESC;

-- 部分更新（PATCH セマンティクス）。NULL の引数は既存値を維持する。
-- 「取得 → 所有者チェック → 更新」ではなく WHERE で絞るのは、
-- チェックと更新の間に競合が入る余地を消すため。
-- name: UpdateTodo :one
UPDATE todos
SET title       = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status      = COALESCE(sqlc.narg('status'), status),
    due_date    = COALESCE(sqlc.narg('due_date'), due_date),
    updated_at  = now()
WHERE id = @id AND user_id = @user_id
RETURNING *;

-- 削除できた行数を返す。0 行なら「存在しない or 他人のもの」= NOT_FOUND。
-- name: DeleteTodo :execrows
DELETE FROM todos
WHERE id = $1 AND user_id = $2;
