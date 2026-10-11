# TODO / 検証待ち事項

設計書 [docs/design.md](docs/design.md) の中で、実装・実験を経ないと確定できない項目を置く。
片付いたら該当セクションへ結論を反映し、ここからは消す。

---

## #1. サブグラフ直叩き + `X-User-ID` 詐称の挙動を検証する

- 出典: design.md §6.8 認可ポリシー表 ケース #12
- 状態: **未検証**
- 優先度: 高（多層防御が機能しているかの確認そのもの）

### 何を検証するか

Cosmo Router を経由せず、todo サブグラフのポート（`:8082`）へ直接 HTTP リクエストを送る。
`Authorization` ヘッダは付けず、`X-User-ID` に他人の UUID を入れる。

```bash
# 前提: ユーザー A・B を作成し、A が TODO を 1 件持っている状態
curl -s localhost:8082/graphql \
  -H 'content-type: application/json' \
  -H "X-User-ID: <ユーザー A の UUID>" \
  -d '{"query": "mutation { createTodo(input: {title: \"なりすまし\"}) { id } }"}'

# 参照側も試す
curl -s localhost:8082/graphql \
  -H 'content-type: application/json' \
  -H "X-User-ID: <ユーザー A の UUID>" \
  -d '{"query": "{ todos { id title } }"}'
```

### 仮説（設計上こうなっているはず）

1. `auth.Middleware` は `Authorization` ヘッダが無いため JWT を検証できず、
   context に `Viewer` を入れないまま次へ進む（§6.6 の「未認証のまま通す」設計）。
2. `createTodo` / `todos` の resolver 冒頭で `auth.ViewerFromContext(ctx)` が
   `ErrUnauthenticated` を返す。
3. `UNAUTHENTICATED` エラーになり、DB には一切到達しない。
4. `X-User-ID` はログ・トレース用のヘッダに過ぎず、**認可の根拠には一度も使われない**
   （§6.5 のコメント、§6.7-5）。

> 注: Router 側は `require_authentication: true` になったため、
> **正規の経路では未認証リクエストがサブグラフに到達することはない**。
> この検証は「Router を迂回された場合の最後の砦」が機能するかを見るもの。

### 確認すべきポイント

- [ ] `X-User-ID` を読み取っているコードが resolver / service / repository のどこにも無いこと
      （`grep -rn "X-User-ID" services/` がミドルウェアのログ出力以外にヒットしないこと）
- [ ] DB にレコードが作られていないこと（`SELECT count(*) FROM todos WHERE title = 'なりすまし'` が 0）
- [ ] エラーが `UNAUTHENTICATED` であり、`INTERNAL_SERVER_ERROR`（nil 参照など）になっていないこと
      — ここが 500 になるなら `ViewerFromContext` のチェック漏れがある
- [ ] 正規の JWT を付けたうえで `X-User-ID` だけ他人に書き換えた場合も、
      **JWT の `sub` が優先**されて自分の TODO しか操作できないこと
- [ ] 同じことを user サブグラフ（`:8081`）の `me` でも試し、
      **他人の ID で `users` に行が作られない**こと（§6.8-#11 と関連）

### 検証後にやること

- design.md §6.8 のケース #12 の「期待する挙動」欄を確定した内容で埋める
- 同じシナリオを E2E テスト（§10）に追加し、CI で常時回す
- §12-10「Router → サブグラフ間の信頼」の要否を判断する。
  HMAC 署名や mTLS を足すかどうかは、この検証で「アプリ層だけで十分守れている」と
  確認できるかにかかっている
- 開発環境で `:8081` / `:8082` を host に公開し続けるかを再検討する。
  本番では ALB に auth と router だけを公開する方針（§11.1）なので、
  開発環境でも `expose` のみにして直叩きを塞ぐ選択肢がある
  — ただしこの検証テスト自体が直叩きを必要とするため、
  テスト用に compose の内部ネットワークから叩く形にする

---

## #2. `@authenticated` の OBJECT 単位適用が composition で通るか確認する

- 出典: design.md §4.4
- 状態: **未検証**
- 優先度: 中（動かなくても回避策はある）

### 何を検証するか

`@authenticated` は `on ENUM | FIELD_DEFINITION | INTERFACE | OBJECT | SCALAR` と
定義されており、OBJECT に付ければ配下の全フィールドをカバーできるはず。
設計書では `type Todo @key(...) @authenticated` のようにエンティティ型へ
OBJECT 単位で付ける前提にしている。

確認したいのは次の 2 点。

1. エンティティ型への OBJECT 単位適用が `wgc router compose` で正しく合成されるか
2. **ルート型（`type Query` / `type Mutation` 自体）への適用**が意図通り動くか
   — 動くなら全ルートフィールドへの個別付与をやめて 2 行に圧縮できる

### 検証方法

```bash
# スキーマを書き換えて合成し、生成された supergraph SDL を確認する
npx wgc router compose -i router/graph.yaml -o /tmp/router.json
jq -r '.engineConfig.graphqlSchema' /tmp/router.json | grep -n "authenticated"
```

合成後のスキーマで、対象フィールドに認証要件が伝播しているかを見る。
そのうえで実リクエスト（トークンなし）を投げ、Router が 401 を返すか確認する。

### 補足

`require_authentication: true` により実質的な防御はすでに効いているため、
これは**自己文書化と二重の保険**の話であって、セキュリティ上の必須項目ではない。
OBJECT 単位が使えないと分かったら、ルートフィールドへの個別付与で確定させる。

---

## #3. `require_authentication: true` 下での Playground / introspection の使い勝手

- 出典: design.md §6.5「開発上の注意」
- 状態: **未検証**
- 優先度: 低（開発体験の問題）

Router 全体を閉じたことで、GraphQL Playground や GraphQL IDE の introspection も
トークンを要求される。実際にどの程度不便かを確認し、必要なら対処を決める。

- [ ] Playground の HTTP ヘッダ欄に `Authorization: Bearer ...` を入れて使えるか
- [ ] Cosmo Router に開発用の例外設定（dev mode 等）があるか調べる
- [ ] 不便なら、開発環境専用の compose override で
      `require_authentication: false` にする案を検討する
      — ただし「開発と本番で認証の強度が違う」状態は §6.5 で得た
      フェイルセーフ性を開発中だけ手放すことになるため、安易に倒さない

---

## #4. Cosmo Router の設定で実装時に判明した差分（記録）

- 出典: design.md §6.5
- 状態: **解決済み**（router v0.346.1 で動作確認）
- 残課題: バージョンを上げたときに再確認する

設計書に書いた設定のうち、実際の router では通らなかったもの。
design.md §6.5 は修正済みだが、経緯を残しておく。

| 項目 | 設計書（当初） | 実際 |
| --- | --- | --- |
| 認証設定のキー | `authentication.providers[]` | `authentication.jwt.jwks[]`（`providers` は config schema に弾かれる） |
| `GRAPH_API_TOKEN` | ダミー値を設定 | **設定してはいけない**。JWT として解釈され `invalid token` で起動失敗 |
| telemetry | 記載なし | 既定で `cosmo-otel.wundergraph.com` へ送信される。明示的に無効化した |
| `router.json` のマウント | ファイル単位 | ディレクトリ単位。生成前は存在しないため、ファイル指定だと Docker がディレクトリを作ってしまう |

Cosmo は更新が速いため、`router:latest` を使っている限りこの種のズレは再発し得る。
安定させるならイメージをバージョン固定する（`ghcr.io/wundergraph/cosmo/router:0.346.1`）。

---

## #5. 署名鍵の無停止ローテーションを実装する

- 出典: design.md §6.2「ローテーション手順（無停止）」
- 状態: **未実装**
- 優先度: 中（本番運用の前に必須）

### 現状

`kid`（RFC 7638 Thumbprint）で鍵を識別する仕組みはあるが、
**JWKS は常に 1 本の鍵しか返さない**（`services/auth/internal/issuer/issuer.go` の `JWKS()`）。
公開鍵は秘密鍵から導出しているため、旧鍵を保持しておく場所もない。

このため、今鍵を差し替えると発行済みトークンが**すべて即座に無効**になる。
これは design.md §6.2 の「緊急時（秘密鍵の漏洩）」の手順と同じ挙動で、
通常の無停止ローテーションはできない。

### やること

- [ ] auth が複数の PEM を読めるようにする（例: ディレクトリ内の `*.pem` をすべて読む）
- [ ] 署名に使う鍵（1 本）と、配信だけする鍵（0 本以上）を区別する
      （例: `JWT_SIGNING_KID` で署名鍵を指定する）
- [ ] `JWKS()` がすべての公開鍵を配列で返すようにする
- [ ] `make gen-keys` に、既存の鍵を消さずに新しい鍵を追加する手段を用意する
- [ ] 手順 ①〜⑥ を通しで実行し、途中で 401 が一度も出ないことを確認する
- [ ] ⑥ で旧鍵を外した後、旧鍵で署名されたトークンが 401 になることを確認する

### 注意

手順「③ 待つ」の待ち時間は、**更新が最も遅い検証者**に合わせる必要がある。
現状は検証者ごとに JWKS の更新間隔が違う（#6）ので、#6 を先に片付ける。

---

## #6. サブグラフ側の JWKS 更新間隔を明示し、Router と揃える

- 出典: design.md §6.2「誰がどう取得するか」
- 状態: **未対応**
- 優先度: 中（#5 の前提）

### 現状

| 検証者 | 更新間隔 | 未知の kid を見たときの即時再取得 |
| --- | --- | --- |
| Cosmo Router | `refresh_interval: 1m`（明示している） | 有効（30 秒間隔・burst 2） |
| user / todo サブグラフ | **`keyfunc` の既定値のまま（値を確認していない）** | **未確認** |

サブグラフは `keyfunc.NewDefaultCtx` を使っており、更新間隔も未知の kid の扱いも
ライブラリの既定値に任せている。その既定値が何かは確認していない。

鍵が 1 本のうちは問題にならない。しかし #5 のローテーションを入れた時点で、
検証者ごとに新しい鍵を認識するタイミングがずれる。

### やること

- [ ] `keyfunc` v3 の既定の更新間隔と、未知の kid で再取得するかどうかを確認する
- [ ] `NewDefaultCtx` をやめ、オプションを指定できるコンストラクタで
      更新間隔（1 分）と未知の kid での再取得を明示する
- [ ] 値を環境変数（例: `JWKS_REFRESH_INTERVAL`）で渡し、Router と同じ値を使う
- [ ] design.md §6.2「誰がどう取得するか」の表に実際の値を反映する

---

## #7. auth 停止中も既存トークンの検証が続くか確認する

- 出典: design.md §6.2「障害時の挙動」
- 状態: **未検証**
- 優先度: 高（可用性の前提そのもの）

### 設計上の想定

auth が停止しても、検証側は**最後に取得できた JWKS を保持し続ける**。
影響は「新規ログインができない」ことだけに限られ、発行済みトークンを使った
GraphQL リクエストは続けられる。
これは ADR 0001 §2.2-D3（可用性の結合）を緩和するための前提になっている。

### 現状

Router と `keyfunc`（サブグラフ側）のどちらについても、この挙動を**実際には確かめていない**。
JWKS の取得に失敗したときにキャッシュを保持するのか、破棄するのか、
検証そのものが失敗するのかが分かっていない。

### 検証方法

```bash
# 1. トークンを取得し、正常に使えることを確認する
TOKEN=$(curl -s localhost:8083/auth/login -H 'content-type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["accessToken"])')
curl -s localhost:3002/graphql -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"query":"{ me { todos { title } } }"}'

# 2. auth を止める
docker compose stop auth

# 3. Router の refresh_interval（1 分）より長く待ってから、同じトークンで叩く
sleep 90
curl -s localhost:3002/graphql -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"query":"{ me { todos { title } } }"}'

# 4. サブグラフを再起動して JWKS キャッシュを空にしてから試す
#    （一度も取得しないうちに auth が落ちているケース）
docker compose restart user-subgraph todo-subgraph
curl -s localhost:3002/graphql -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"query":"{ me { todos { title } } }"}'

docker compose start auth
```

※ トークンの有効期間は 15 分なので、1 から 4 までをその中に収める。

### 確認すべきポイント

- [ ] 3 で、Router が JWKS の取得に失敗した後もキャッシュでトークンを検証し続けるか
- [ ] 3 で、サブグラフ側（`keyfunc`）も同じように検証し続けるか
- [ ] 4 のケース（キャッシュが空のまま auth が落ちている）では失敗して当然だが、
      エラーが `UNAUTHENTICATED` になり、500 や panic にならないこと
- [ ] Router とサブグラフのログに JWKS の取得失敗が記録されること

### 検証後にやること

- 想定どおりでなければ、検証側に「最後に成功した JWKS を保持する」仕組みを追加する
- design.md §6.2「障害時の挙動」を検証結果で確定させる

---

## #8. 本番では JWKS の取得経路を TLS か VPC 内部通信にする

- 出典: design.md §6.2「セキュリティ上の注意」1
- 状態: **未対応**（開発環境では意図的に平文）
- 優先度: 低（本番構成に着手するときに対応）

### 現状

開発環境では、Router とサブグラフが compose ネットワーク内の平文 HTTP
（`http://auth:8083/.well-known/jwks.json`）で JWKS を取得している。
開発用途としては設計どおり。

### なぜ必要か

JWKS は公開鍵なので機密性は要らないが、**完全性**は欠かせない。
取得経路を中間者に差し替えられると、攻撃者は自分の鍵を JWKS に紛れ込ませ、
任意のユーザーのトークンを偽造できてしまう。

### やること

- [ ] 本番構成（design.md §11）で、JWKS の取得経路を次のどちらかにする
  - auth を ALB 経由の HTTPS で公開し、検証側は `https://` の URL を使う
  - ECS Service Connect / VPC 内部通信に閉じ、外部から到達できないようにする
- [ ] サブグラフの `JWKS_URL` と Router の `authentication.jwt.jwks[].url` が
      本番で `http://` になっていないことを、デプロイ時にチェックする

---

## #9. Web フロントエンドの既知の制約

- 出典: design.md §4.7
- 状態: **記録**（リフレッシュトークン導入時にまとめて見直す）
- 優先度: 中

### 15 分で再ログインになる

リフレッシュトークン（design.md §12-1）を後回しにして web を先に作った。そのため、
アクセストークンの期限（15 分）が切れると、Router が 401 を返し、web は Cookie を消して `/login` へ戻す。
操作中でも切れるため、フォームに入力中の内容は失われる。

- [ ] §12-1 を実装したら、web は Router の 401 を受けた時点で `POST /auth/refresh` を呼び、
      成功すれば元のリクエストをやり直すようにする
- [ ] ログアウトでリフレッシュトークンの行を DELETE する（design.md §12-2）

### 期限（dueDate）を消せない

`UpdateTodo` は `COALESCE(sqlc.narg(...), 既存値)` の PATCH 方式のため、NULL を渡すと
「変更しない」になる。編集画面で期限を空にしても、元の期限が残る。

- [ ] `UpdateTodoInput` に `clearDueDate: Boolean` を足すか、値の「未指定」と「null」を
      区別できる形（gqlgen の `omittable`）にするかを決める

---

## 参考: 設計書側で「後回し」と明記した項目

以下は design.md §12「未決事項・今後の拡張」に一覧がある。実装フェーズで
判断が必要になった時点でこちらへ引き上げる。

- **リフレッシュトークン（§12-1）— 優先度 高。** アクセストークンを 15 分にしたため
  （§6.1）、FE クライアントに着手する前に必ず必要になる。
  §6.8-#9 の「認証失敗の理由を区別しない」方針の再検討とセット
- ログアウト / トークン失効（§12-2）— 上のリフレッシュトークンを `auth_db.refresh_tokens` として
  実装すれば同時に解決する（ログアウト＝行の DELETE）。`jti` ブロックリストは不要で、
  「Redis 不要」の前提とも衝突しない
- ページネーション（§12-3 / §4.6）
- サブグラフ内の DataLoader（§12-5）
- 未プロビジョニングユーザーの扱い（§12-8）— JIT provisioning のトレードオフ
- 論理削除 `deleted_at` の要否（§12-9）— 入れるなら全クエリに条件が増えるため早期に決める
- Router → サブグラフ間の信頼（§12-10）— #1 の検証結果次第
