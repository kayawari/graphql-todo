# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

GraphQL Federation（WunderGraph Cosmo Router）による TODO アプリのバックエンド。
設計の真実源は `docs/design.md`（本文中の「design.md §x.y」参照はこれを指す）、
決定記録は `docs/adr/`、実験しないと確定できない事項は `todo.md`。ドキュメント・コメント・コミットメッセージは日本語。

## コマンド

```bash
make up          # 鍵生成 → compose 起動（DB・migrate・3 サービス・router compose・router）→ シード投入
make up-proxy    # Envoy 込みで :8080 に入口を一本化
make down        # 停止 + ボリューム削除（次回はまっさらから）
make generate    # sqlc + gqlgen を 3 サービス分再生成（go tool 経由。別途インストール不要）
make router      # router.json を再合成して Router を再起動
make build / make test / make tidy   # 3 サービスに対して go build / go test / go mod tidy
make migrate-new SVC=todo NAME=add_priority
```

- 各サービス（`services/{auth,user,todo}`）は**独立した Go モジュール**（go.work なし）。Go コマンドはサービスのディレクトリ内で実行する。
  単一テスト: `cd services/todo && go test ./internal/service -run TestName`
- 自動テストはまだ無い（テスト方針は design.md §10）。
- GraphQL スキーマ（`services/{user,todo}/graph/schema.graphqls`）を変えたら `make generate && make router`。再合成しないと Router は古いスーパーグラフのまま動く。
- SQL（`db/<svc>/queries/*.sql` やマイグレーション）を変えたら `make sqlc`。sqlc はマイグレーションディレクトリをスキーマとして読む。マイグレーション適用は `make down && make up` が最速。
- `make gen-keys` は意図的に冪等。`secrets/dev/jwt_private.pem` を消して再生成すると `kid` が変わり発行済みトークンが全滅する。

## アーキテクチャ

```
Client ─(Envoy :8080, 任意)─┬─ /auth/*  → auth :8083（REST・グラフ外。署名と JWKS 配信）
                            └─ /graphql → Cosmo Router :3002 ─┬─ user サブグラフ :8081（User 所有）
                                                               └─ todo サブグラフ :8082（Todo 所有、User.todos を拡張）
PostgreSQL: auth_db / user_db / todo_db（ロールごとに分離、相互 CONNECT 剥奪）
```

- **auth はグラフに参加しない**（ADR 0001）。そのおかげで Router を `require_authentication: true` で閉じられる。Router に公開オペレーションを追加する設計は取らない。
- **3 つの DB は同じユーザー UUID を共有する規約だけで結び付いている**（FK なし）。user_db の行は `me` 初回アクセス時の JIT provisioning（`services/user/internal/service/provisioning.go`）で作られる。シード（`scripts/seed.sh`）も SQL 直投入ではなく REST + GraphQL 経由で投入する。
- `User.email` は user_db に保存せず JWT claims から解決する。
- Router の設定は `router/config.yaml`、合成入力は `router/graph.yaml`（docker compose の `compose.yaml` と紛らわしいので別名）。`router/router.json` は生成物で gitignore 対象。Router はコントロールプレーンなしで動かしている。Cosmo の設定キーが設計書と実物で違った経緯は `todo.md` #4。

### サブグラフの層構成（user / todo 共通）

`cmd/server/main.go`（配線） → `internal/auth`（JWT 検証ミドルウェア） → `graph/*.resolvers.go`（gqlgen、`follow-schema` レイアウト） → `internal/service`（ドメイン） → `internal/db`（sqlc 生成）

- 生成物: `graph/generated/`（exec と federation は同一パッケージ必須）、`graph/model/models_gen.go`、`internal/db/*.sql.go`。手で編集しない。
- `@authenticated` は Router が解釈するディレクティブ。gqlgen.yml で `skip_runtime: true` にしている。
- `internal/auth` と `internal/apperr` は user / todo でコピーしている（意図的な重複。3 つ目の利用者が出たら共有化を検討）。片方を直したらもう片方も揃える。
- サブグラフの JWKS は起動時ではなく初回検証時に遅延取得する（auth の起動を待たないため）。
- エラーは `internal/apperr` で `extensions.code` 付きの gqlerror にする。内部エラーの詳細はログにだけ残す。

### 認可ルール（破ると即情報漏洩。design.md §6.7）

1. sqlc クエリは必ず `user_id` で絞る（`WHERE id = $1 AND user_id = $2`）。UPDATE / DELETE も「取得 → 所有者チェック → 更新」ではなく WHERE で絞る。service 層のメソッドは必ず userID を引数に取る。
2. 他人のリソースは `FORBIDDEN` ではなく `NOT_FOUND`（存在推測を防ぐ）。
3. entity resolver（`entity.resolvers.go`）は Router から任意の ID で呼ばれ得る。`User.todos` では `user.ID == viewer.UserID` を検証し、他人の ID では provisioning を走らせない。
4. `user_id` を入力（`CreateTodoInput` 等）から受け取らない。常に `auth.ViewerFromContext(ctx)` の viewer から取る。
5. `X-User-ID` ヘッダは Router が注入するログ用の値。認可の根拠に使わない。
6. サブグラフのミドルウェアは検証失敗でも 401 を返さず未認証のまま通す。認証が必要な resolver は先頭で必ず `auth.ViewerFromContext` を呼ぶ。
7. JWT 検証は `RS256` 固定・issuer / audience 検証必須。

新しい resolver を足すときは「A のトークンで B のリソースに触れない」「他人の ID で entity resolver を叩いても users に行が作られない」をセットで確認する（design.md §10）。
