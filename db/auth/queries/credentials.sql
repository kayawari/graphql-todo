-- name: CreateCredential :one
INSERT INTO credentials (email, password_hash)
VALUES ($1, $2)
RETURNING *;

-- name: GetCredentialByEmail :one
SELECT * FROM credentials
WHERE email = $1;
