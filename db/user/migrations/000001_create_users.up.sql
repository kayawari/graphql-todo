-- design.md §5.2
CREATE TABLE users (
    -- auth サービスが採番した ID を受け取る。DEFAULT を置かない。
    id           UUID PRIMARY KEY,
    display_name TEXT        NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 50),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- email と password_hash は持たない。
-- email は認証の識別子であり user サブグラフのドメインではないため、
-- User.email は JWT claims から解決する（design.md §4.2）。
