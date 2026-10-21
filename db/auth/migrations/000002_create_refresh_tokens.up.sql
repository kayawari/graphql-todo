-- design.md §5.1 / design.md §6.9
--
-- トークンそのものは保存せず、SHA-256 ハッシュだけを持つ。DB が漏れても
-- そのまま使える値は出ない。32 バイトの乱数なので bcrypt のような遅いハッシュは要らない。
CREATE TABLE refresh_tokens (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- ログイン 1 回ごとに採番し、ローテーションで引き継ぐ。
    -- 再利用を検知したら、この単位で失効させる。
    family_id         UUID        NOT NULL,
    -- credentials と同じ DB にあるので FK を張れる（DB をまたぐ関連とは違い規約に頼らない）。
    user_id           UUID        NOT NULL REFERENCES credentials (id) ON DELETE CASCADE,
    token_hash        BYTEA       NOT NULL UNIQUE,
    -- スライドする期限。ただし family_expires_at を超えない。
    expires_at        TIMESTAMPTZ NOT NULL,
    -- ログインからの絶対上限。ファミリー内で引き継ぐ。
    family_expires_at TIMESTAMPTZ NOT NULL,
    -- ローテーション済みの印。NULL でない行が再び提示されたら再利用。
    used_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_family_id_idx ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);
