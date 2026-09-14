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

## 参考: 設計書側で「後回し」と明記した項目

以下は design.md §12「未決事項・今後の拡張」に一覧がある。実装フェーズで
判断が必要になった時点でこちらへ引き上げる。

- **リフレッシュトークン（§12-1）— 優先度 高。** アクセストークンを 15 分にしたため
  （§6.1）、FE クライアントに着手する前に必ず必要になる。
  §6.8-#9 の「認証失敗の理由を区別しない」方針の再検討とセット
- ログアウト / トークン失効（§12-2）— 上の #1 を `auth_db.refresh_tokens` として
  実装すれば同時に解決する（ログアウト＝行の DELETE）。`jti` ブロックリストは不要で、
  「Redis 不要」の前提とも衝突しない
- ページネーション（§12-3 / §4.6）
- サブグラフ内の DataLoader（§12-5）
- 未プロビジョニングユーザーの扱い（§12-8）— JIT provisioning のトレードオフ
- 論理削除 `deleted_at` の要否（§12-9）— 入れるなら全クエリに条件が増えるため早期に決める
- Router → サブグラフ間の信頼（§12-10）— #1 の検証結果次第
