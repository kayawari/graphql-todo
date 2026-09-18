-- postgres コンテナの初回起動時に superuser として実行される。
-- design.md §5.4「接続ユーザーの分離」の実装。
--
-- 開発用のパスワードをここに直書きしている。本番では Secrets Manager から
-- 注入したうえで、この種の初期化は IaC 側で行う。

-- ---- サービスロール ----
CREATE ROLE auth_svc LOGIN PASSWORD 'auth_pass';
CREATE ROLE user_svc LOGIN PASSWORD 'user_pass';
CREATE ROLE todo_svc LOGIN PASSWORD 'todo_pass';

-- ---- 論理 DB（1 コンテナ内で分離）----
CREATE DATABASE auth_db OWNER auth_svc;
CREATE DATABASE user_db OWNER user_svc;
CREATE DATABASE todo_db OWNER todo_svc;

-- ---- 相互接続の遮断 ----
-- PostgreSQL は PUBLIC ロールに CONNECT を既定で与えるため、
-- 個別ロールから REVOKE しても PUBLIC 経由で接続できてしまう。
-- 「PUBLIC から剥奪 → 所有者にだけ GRANT」が正しい順序。
REVOKE CONNECT ON DATABASE auth_db FROM PUBLIC;
REVOKE CONNECT ON DATABASE user_db FROM PUBLIC;
REVOKE CONNECT ON DATABASE todo_db FROM PUBLIC;

GRANT CONNECT ON DATABASE auth_db TO auth_svc;
GRANT CONNECT ON DATABASE user_db TO user_svc;
GRANT CONNECT ON DATABASE todo_db TO todo_svc;

-- ---- 拡張（superuser でないと作れないため、マイグレーションではなくここで）----
\connect auth_db
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

\connect todo_db
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- user_db は拡張不要。
-- id は auth が採番した値を受け取るため gen_random_uuid() を使わず、
-- email を持たないため citext も要らない（design.md §5.2）。
