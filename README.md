# graphql-todo

GraphQL Federation（WunderGraph Cosmo）による TODO アプリケーションのバックエンド。

- 設計: [docs/design.md](docs/design.md)
- 決定記録: [docs/adr/](docs/adr/)
- 検証待ち: [todo.md](todo.md)

## 構成

```
Client ─┬─ (profile: proxy) ─ Envoy :8080 ─┬─ /auth/*  ─→ auth      :8083  REST・グラフ外
        │                                  └─ /graphql ─→ Router    :3002
        ├─ auth   :8083                                    ├─→ user サブグラフ :8081
        └─ Router :3002                                    └─→ todo サブグラフ :8082
                                       PostgreSQL :5432  auth_db / user_db / todo_db
```

`auth` は GraphQL グラフに参加しない。これにより Router を
`require_authentication: true` で閉じられる（[ADR 0001](docs/adr/0001-authentication-service-separation.md)）。

---

## 開発環境の立ち上げ

### 前提

| 必要なもの | 用途 |
| --- | --- |
| Docker / Docker Compose | 全サービスの実行 |
| openssl | 開発用の署名鍵生成（`make gen-keys`） |
| curl, python3 | シード投入（`scripts/seed.sh`） |
| Go 1.26 | コード生成・ローカルビルド（起動するだけなら不要） |

### 手順

```bash
cd graphql-todo
cp .env.example .env   # 任意。既定値のままでも動く
make up
```

`make up` の 1 コマンドで、次が順に実行される。

| # | 段階 | 内容 |
| --- | --- | --- |
| 1 | **鍵生成** | `secrets/dev/jwt_private.pem` を作る。既にあれば何もしない（再生成すると `kid` が変わり発行済みトークンが全て無効になるため、意図的に冪等） |
| 2 | **DB 起動** | postgres を起動し、`db/init/00-databases.sql` で 3 つの論理 DB と 3 つのロールを作成。ロール間の `CONNECT` は相互に剥奪される |
| 3 | **マイグレーション** | golang-migrate が `auth_db` / `user_db` / `todo_db` にそれぞれ適用（one-shot コンテナ 3 つ） |
| 4 | **サービス起動** | auth / user サブグラフ / todo サブグラフ。各 `/health` が healthy になるまで待つ |
| 5 | **スキーマ合成** | `wgc router compose` が 2 つのサブグラフを introspect し `router/router.json` を生成（control plane 不要） |
| 6 | **Router 起動** | 生成された `router.json` を読んで :3002 で待ち受ける |
| 7 | **シード投入** | `scripts/seed.sh` がユーザー 2 名と TODO 5 件を API 経由で投入 |

完了すると次が使える状態になる。

| | URL |
| --- | --- |
| ログイン / サインアップ | http://localhost:8083/auth/login, `/auth/signup` |
| GraphQL | http://localhost:3002/graphql |
| GraphQL Playground | http://localhost:3002/ |
| Router のメトリクス | http://localhost:8088/metrics |

Envoy 込みで起動する場合は `make up-proxy`。入口が :8080 に一本化され、
パスルーティング・ログインのレート制限・401 の正規化が有効になる。

停止は `make down`（ボリュームも削除するので次回はまっさらな状態から立ち上がる）。

### シードデータ

| ユーザー | パスワード | TODO |
| --- | --- | --- |
| alice@example.com | password123 | 3 件（うち 1 件 DONE） |
| bob@example.com | password123 | 2 件 |

2 ユーザー用意してあるのは、**「alice のトークンで bob の TODO が見えない」を
すぐ手で確認できるようにするため**（design.md §6.8-#5）。

シードは SQL を直接流し込まず、**auth の REST と Router の GraphQL を経由する**。
`credentials` / `users` / `todos` は 3 つの別々の DB にあり、同じ UUID を共有する
規約だけで結び付いている。SQL で直接入れるとその規約をシード側で複製することになり、
サービスが持つべき責務がシードスクリプトへ漏れる。API 経由なら
JIT provisioning（design.md §6.4）もそのまま通るので、開発環境の疎通確認も兼ねる。

冪等なので何度実行してもよい。単体で回すなら `make seed`、
投入を飛ばすなら `SKIP_SEED=1 make up`。

### 動作確認

```bash
# トークンを取る
TOKEN=$(curl -s localhost:8083/auth/login -H 'content-type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["accessToken"])')

# Federation を横断するクエリ
#   me / email        → user サブグラフ（email は JWT claims から解決）
#   todos             → todo サブグラフ（User エンティティ経由で解決）
curl -s localhost:3002/graphql -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"query":"{ me { id email displayName todos { title status } } }"}'

# 未認証だと Router が 401 を返す（require_authentication: true）
curl -i -s localhost:3002/graphql -H 'content-type: application/json' \
  -d '{"query":"{ todos { id } }"}'
```

`require_authentication: true` のため、GraphQL Playground や IDE の introspection も
トークンを要求する。Playground の HTTP ヘッダ欄に
`Authorization: Bearer ...` を設定して使う（[todo.md](todo.md) #3）。

---

## 開発

```bash
make generate   # sqlc + gqlgen の生成物を作り直す
make build      # 3 サービスを go build
make test       # 3 サービスを go test
make router     # スキーマ変更後に router.json を再生成して Router を再起動
make logs       # 全サービスのログを追う
make help       # 全ターゲット一覧
```

GraphQL スキーマ（`services/*/graph/schema.graphqls`）を変えたら
`make generate && make router` を実行する。合成をやり直さないと Router が
古いスキーマのまま動く。

### マイグレーションの追加

```bash
make migrate-new SVC=todo NAME=add_priority
# db/todo/migrations/ に up/down の雛形ができる
make down && make up   # 適用（開発中はこれが一番早い）
```

---

## ディレクトリ

| パス | 内容 |
| --- | --- |
| `db/init/` | DB・ロール作成（postgres の初回起動時に実行） |
| `db/{auth,user,todo}/migrations/` | golang-migrate のマイグレーション |
| `db/{auth,user,todo}/queries/` | sqlc の入力 SQL |
| `scripts/seed.sh` | 開発用シードデータ（API 経由・冪等） |
| `router/graph.yaml` | wgc router compose の入力（サブグラフ一覧） |
| `router/config.yaml` | Cosmo Router 設定 |
| `envoy/envoy.yaml` | Envoy 設定（パスルーティング・レート制限・401 正規化） |
| `services/auth/` | 認証サービス（REST、グラフ外） |
| `services/user/` | user サブグラフ（User エンティティ所有） |
| `services/todo/` | todo サブグラフ（Todo 所有、User.todos を拡張） |

## 実装状況

| 機能 | 状態 |
| --- | --- |
| サインアップ / ログイン / JWKS 配信 | 実装済み・動作確認済み |
| Router の JWT 検証・`require_authentication` | 実装済み・動作確認済み |
| サブグラフ側の JWT 再検証 | 実装済み |
| TODO の CRUD | 実装済み・動作確認済み |
| `me` の JIT provisioning | 実装済み・動作確認済み |
| `User.todos`（Federation の entity 解決） | 実装済み・動作確認済み |
| 他人のリソースが `NOT_FOUND` になること | 動作確認済み |
| シードデータ | 実装済み・冪等性確認済み |
| 自動テスト | **未着手**（design.md §10） |
| リフレッシュトークン | **未着手**（design.md §12-1。FE 着手前に必要） |
