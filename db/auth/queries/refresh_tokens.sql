-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (family_id, user_id, token_hash, expires_at, family_expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetRefreshTokenForUpdate :one
-- 同じトークンでの並行リフレッシュを直列化する。後から来た側は used_at が
-- 付いた行を見るので、猶予内かどうかで判定できる（design.md §6.9）。
SELECT rt.id, rt.family_id, rt.user_id, rt.expires_at, rt.family_expires_at, rt.used_at,
       c.email
FROM refresh_tokens rt
JOIN credentials c ON c.id = rt.user_id
WHERE rt.token_hash = $1
FOR UPDATE OF rt;

-- name: MarkRefreshTokenUsed :exec
UPDATE refresh_tokens
SET used_at = $2
WHERE id = $1;

-- name: DeleteRefreshTokenFamily :exec
DELETE FROM refresh_tokens
WHERE family_id = $1;

-- name: DeleteRefreshTokenFamilyByHash :exec
DELETE FROM refresh_tokens
WHERE family_id = (SELECT t.family_id FROM refresh_tokens t WHERE t.token_hash = $1);

-- name: DeleteExpiredRefreshTokens :exec
-- ファミリーの上限を過ぎた行だけを消す。上限内の used_at 付きの行は
-- 再利用の検知に使うので残す。
DELETE FROM refresh_tokens
WHERE user_id = $1 AND family_expires_at < now();
