-- name: GetUser :one
SELECT * FROM users
WHERE id = $1;

-- JIT provisioning（design.md §6.4）。
-- ON CONFLICT DO NOTHING のため、既に行が存在すると 0 行返る点に注意。
-- name: ProvisionUser :one
INSERT INTO users (id, display_name)
VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: UpdateDisplayName :one
UPDATE users
SET display_name = $2,
    updated_at   = now()
WHERE id = $1
RETURNING *;
