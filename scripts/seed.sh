#!/usr/bin/env bash
#
# 開発用のシードデータを投入する（design.md §8.7）。
#
# SQL を直接流し込まず、auth の REST と Router の GraphQL を経由する。
# credentials / users / todos は 3 つの別々の DB にあり、同じ UUID を共有する
# という規約だけで結び付いている（design.md §5）。SQL で直接入れると
# その規約をシード側で手作業で複製することになり、サービスが所有すべき
# 責務がシードスクリプトへ漏れ出す。
#
# API 経由なら JIT provisioning（design.md §6.4）もそのまま通るため、
# 「開発環境が実際に動くこと」の検証を兼ねる。
#
# 冪等。既にユーザーや TODO があれば作り直さない。

set -euo pipefail

AUTH_BASE_URL="${AUTH_BASE_URL:-http://localhost:8083}"
GRAPHQL_URL="${GRAPHQL_URL:-http://localhost:3002/graphql}"
SEED_PASSWORD="${SEED_PASSWORD:-password123}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# ---- ユーティリティ ----

# jget 'a.b.0.c' < file  で JSON を掘る。見つからなければ空文字。
jget() {
  python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for k in sys.argv[1].split("."):
    if d is None:
        break
    if isinstance(d, list):
        i = int(k)
        d = d[i] if i < len(d) else None
    elif isinstance(d, dict):
        d = d.get(k)
    else:
        d = None
print("" if d is None else d)
' "$1"
}

wait_for() {
  local url="$1" name="$2" i
  for i in $(seq 1 90); do
    if curl -s -o /dev/null "$url"; then
      return 0
    fi
    sleep 1
  done
  echo "seed: $name ($url) が応答しません" >&2
  return 1
}

gql() {
  curl -s "$GRAPHQL_URL" \
    -H "authorization: Bearer $1" \
    -H 'content-type: application/json' \
    -d "$2"
}

# GraphQL は errors があっても HTTP 200 を返すので明示的に見る。
gql_check() {
  local err
  err="$(printf '%s' "$2" | jget 'errors.0.message')"
  if [ -n "$err" ]; then
    echo "seed: $1 に失敗: $err" >&2
    printf '%s\n' "$2" >&2
    return 1
  fi
}

# ---- 1 ユーザー分のシード ----

seed_user() {
  local email="$1" display="$2"
  shift 2
  local titles=("$@")
  local code token resp current existing title first_id

  # signup。既に居れば 409 が返るので login にフォールバックする（冪等性）。
  code="$(curl -s -o "$TMP/signup.json" -w '%{http_code}' "$AUTH_BASE_URL/auth/signup" \
    -H 'content-type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$SEED_PASSWORD\",\"displayName\":\"$display\"}")"

  case "$code" in
    201)
      token="$(jget accessToken < "$TMP/signup.json")"
      echo "  created  $email"
      ;;
    409)
      curl -s -o "$TMP/login.json" "$AUTH_BASE_URL/auth/login" \
        -H 'content-type: application/json' \
        -d "{\"email\":\"$email\",\"password\":\"$SEED_PASSWORD\"}"
      token="$(jget accessToken < "$TMP/login.json")"
      echo "  exists   $email"
      ;;
    *)
      echo "seed: signup が想定外の応答 (HTTP $code)" >&2
      cat "$TMP/signup.json" >&2
      return 1
      ;;
  esac

  if [ -z "$token" ]; then
    echo "seed: $email のトークンを取得できませんでした" >&2
    return 1
  fi

  # me を叩くと user_db 側の行が JIT provisioning で作られる（design.md §6.4）。
  resp="$(gql "$token" '{"query":"{ me { id displayName } }"}')"
  gql_check "me ($email)" "$resp"

  # login のトークンは display_name claim を持たない。既存ユーザーを初めて
  # provisioning した場合は既定値になるので、ここで揃えておく。
  current="$(printf '%s' "$resp" | jget 'data.me.displayName')"
  if [ "$current" != "$display" ]; then
    resp="$(gql "$token" "{\"query\":\"mutation { updateProfile(displayName: \\\"$display\\\") { displayName } }\"}")"
    gql_check "updateProfile ($email)" "$resp"
  fi

  # 既に TODO があればスキップ
  resp="$(gql "$token" '{"query":"{ todos { id } }"}')"
  gql_check "todos ($email)" "$resp"
  existing="$(printf '%s' "$resp" | jget 'data.todos.0.id')"
  if [ -n "$existing" ]; then
    echo "  skip     $email の TODO は投入済み"
    return 0
  fi

  first_id=""
  for title in "${titles[@]}"; do
    resp="$(gql "$token" "{\"query\":\"mutation { createTodo(input: {title: \\\"$title\\\"}) { id } }\"}")"
    gql_check "createTodo ($title)" "$resp"
    if [ -z "$first_id" ]; then
      first_id="$(printf '%s' "$resp" | jget 'data.createTodo.id')"
    fi
  done
  echo "  seeded   $email に TODO を ${#titles[@]} 件"

  # 1 件だけ DONE にして、status で絞り込めることを確認できるようにする。
  if [ -n "$first_id" ]; then
    resp="$(gql "$token" "{\"query\":\"mutation { updateTodo(id: \\\"$first_id\\\", input: {status: DONE}) { id status } }\"}")"
    gql_check "updateTodo(DONE)" "$resp"
  fi
}

# ---- 実行 ----

echo "seed: サービスの起動を待っています..."
wait_for "$AUTH_BASE_URL/health" "auth"
wait_for "$GRAPHQL_URL" "router"

echo "seed: 投入中 (password: $SEED_PASSWORD)"

seed_user "alice@example.com" "Alice" \
  "設計書をレビューする" \
  "docker compose の起動を確認する" \
  "E2E テストを追加する"

seed_user "bob@example.com" "Bob" \
  "認可の回帰テストを書く" \
  "リフレッシュトークンを検討する"

cat <<EOF

seed: 完了

  ユーザー: alice@example.com / bob@example.com  (password: $SEED_PASSWORD)

  トークン取得:
    curl -s $AUTH_BASE_URL/auth/login -H 'content-type: application/json' \\
      -d '{"email":"alice@example.com","password":"$SEED_PASSWORD"}'

  2 ユーザー用意してあるのは、「alice のトークンで bob の TODO が見えないこと」を
  手で確認できるようにするため（design.md §6.8-#5）。
EOF
