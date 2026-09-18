-- design.md §5.3
CREATE TYPE todo_status AS ENUM ('TODO', 'DONE');

CREATE TABLE todos (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- user_db.users.id を指すが、別サービス所有のため FK 制約は張らない
    user_id     UUID        NOT NULL,
    title       TEXT        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description TEXT,
    status      todo_status NOT NULL DEFAULT 'TODO',
    due_date    DATE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 全てのクエリが user_id で絞り込まれるため、user_id を先頭に置く。
-- この順序でなければ認可の WHERE 句にインデックスが効かない。
CREATE INDEX todos_user_id_created_at_idx ON todos (user_id, created_at DESC);
CREATE INDEX todos_user_id_status_idx     ON todos (user_id, status);
