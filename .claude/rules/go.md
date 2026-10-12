---
paths:
  - "services/**/*.go"
---

# Go の書き方の規約

Go のデファクトスタンダードに従う。基準は [Effective Go](https://go.dev/doc/effective_go)、
[Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)、[Google Go Style Guide](https://google.github.io/styleguide/go/)。
迷ったら標準ライブラリの書き方に倣う。認可ルールはここではなく CLAUDE.md「認可ルール」に従う。

## 整形・静的検査

- `gofmt` / `goimports` 済みであること。`go vet ./...` を通す。
- import は「標準ライブラリ / 外部 / 自モジュール」の 3 グループを空行で区切る（`services/todo/cmd/server/main.go` の形）。

## 命名

- MixedCaps。イニシャリズムは大文字で揃える（`ID`, `URL`, `JWT`, `HTTP`。`Id` や `Url` にしない）。
- パッケージ名は短い小文字の単数形。パッケージ名と識別子の重複（stutter）を避ける（`apperr.NotFound` は良い、`apperr.AppErrNotFound` は悪い）。
- `util` / `common` / `helpers` のような意味の無いパッケージを作らない。
- レシーバ名は型名由来の 1〜2 文字で、その型のメソッド間で統一する（既存: `s *TodoService`, `h *Handler`, `i *Issuer`）。`this` / `self` は使わない。
- getter に `Get` を付けない（`KeyID()` であって `GetKeyID()` ではない）。ただし sqlc 生成の `GetTodo` 等や、DB 取得を意味する service のメソッドはこの限りでない。
- 変数名はスコープが狭いほど短く、広いほど説明的にする。

## エラー

- エラーは必ず扱う。`_ = f()` で黙って捨てない（捨てるなら理由をコメントに書く）。
- 文言は小文字始まり・末尾に句点なし。固定文言は `errors.New`、値を埋めるときだけ `fmt.Errorf`。
- 文脈を足すときは `fmt.Errorf("load private key: %w", err)` の形でラップする（`services/auth/internal/issuer/issuer.go`）。
- 判定は `errors.Is` / `errors.As`。`==` や文字列比較で判定しない。
- 呼び出し側が判定するエラーはセンチネル `var ErrXxx = errors.New(...)` で公開する（`service.ErrNotFound`）。
- 同じエラーを「ログに出してから返す」二重処理をしない。ログ化は境界（resolver / HTTP handler）で 1 回だけ。
  サブグラフでは `apperr.Internal`、auth では `Handler.internalError` を使う。
- `panic` はプログラミングエラーにだけ使う。リクエスト処理中の失敗は error で返す。

## context

- I/O やブロックし得る関数は第 1 引数に `ctx context.Context` を取る。
- context を構造体のフィールドに保持しない。
- `context.Background()` / `context.TODO()` は `main` とテストだけで使い、それ以外は受け取った ctx を引き回す。

## インターフェース

- インターフェースは使う側のパッケージで定義し、必要なメソッドだけの小さなものにする。
- 「引数は interface、戻り値は具象型」。実装の隣にモック目的で先回りの interface を作らない。

## 並行処理

- goroutine を起動するなら終了経路（ctx キャンセル、チャネル close 等）を必ず持たせる。
- 複数 goroutine のエラー集約は `golang.org/x/sync/errgroup` を優先する。
- 共有状態は `sync.Mutex` かチャネルで保護を明示し、`go test -race` が通る形にする。

## ログ

- `log/slog` の構造化ログだけを使う。`fmt.Print*` や標準 `log` パッケージでログを出さない。
- メッセージは小文字の短い英語、値は key-value で渡す。キーは既存に合わせる（`"err"`, `"op"`, `"addr"`）。
- パスワード・トークン・秘密鍵などの秘密情報をログに出さない。

## コメント

- エクスポートした識別子には、その名前で始まる doc コメントを付ける。パッケージ doc はパッケージ内の 1 ファイルにだけ書く。
- コメントは日本語。「何をしているか」より「なぜそうしているか」を書き、根拠があれば design.md / ADR の節を参照する（既存コードの書き方）。

## 構成

- 外部に公開しないコードは `internal/` 配下に置く。
- `main` は `run(...) error` に委譲し、`os.Exit` は `main` でだけ呼ぶ（`cmd/server/main.go` の形）。
- パッケージレベルの可変グローバル変数と、`init()` での副作用を避ける。依存はコンストラクタ（`New`）で注入する。

## 依存

- 標準ライブラリを優先する。新しい外部依存を足すときは理由を説明してから追加する。
- 開発ツールは `go.mod` の `tool` ディレクティブ経由で使う（`go tool sqlc`, `go tool gqlgen`）。
- 依存を変えたら `go mod tidy` を実行し、`go.mod` / `go.sum` の差分も残す。

## テスト（design.md §10）

- テーブル駆動テストと `t.Run` によるサブテストを基本にする。
- テストヘルパーには `t.Helper()` を付ける。失敗メッセージは `got X, want Y` の順で書く。
- context は `t.Context()` を使う。構造体の比較に `go-cmp` を使う場合は導入前に相談する。

## 生成物

- `graph/generated/`、`graph/model/models_gen.go`、`internal/db/*.sql.go` は手で編集しない。
  規約に合わない箇所があっても、元（GraphQL スキーマ / SQL / `gqlgen.yml` / `sqlc.yaml`）を直して `make generate` する。

## 完了前のチェック

変更したサービスのディレクトリで以下を実行し、すべて通ることを確認する。

```bash
gofmt -l .        # 出力が空であること
go vet ./...
go build ./...
go test ./...
```
