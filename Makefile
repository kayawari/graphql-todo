SHELL := /bin/bash
SERVICES := auth user todo
SUBGRAPHS := user todo
KEY_DIR := secrets/dev
KEY := $(KEY_DIR)/jwt_private.pem

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---- 鍵 ----
.PHONY: gen-keys
gen-keys: $(KEY) ## 開発用 RSA 鍵ペアを生成（冪等。既にあれば何もしない）

# 再生成すると kid が変わり、発行済みトークンが全て無効になるうえ
# Router の JWKS キャッシュも古くなる。だから冪等にしてある（design.md §6.2）。
$(KEY):
	@mkdir -p $(KEY_DIR)
	openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out $(KEY)
	@chmod 600 $(KEY)
	@echo "generated $(KEY)"

# ---- コード生成 ----
.PHONY: generate
generate: sqlc gqlgen ## sqlc と gqlgen の生成物をまとめて作り直す

.PHONY: sqlc
sqlc: ## sqlc generate（3 サービス）
	@for s in $(SERVICES); do \
		echo "==> sqlc: $$s"; \
		(cd services/$$s && go tool sqlc generate) || exit 1; \
	done

.PHONY: gqlgen
gqlgen: ## gqlgen generate（2 サブグラフ）
	@for s in $(SUBGRAPHS); do \
		echo "==> gqlgen: $$s"; \
		(cd services/$$s && go tool gqlgen generate) || exit 1; \
	done

.PHONY: tidy
tidy: ## go mod tidy（3 サービス）
	@for s in $(SERVICES); do (cd services/$$s && go mod tidy) || exit 1; done

.PHONY: build
build: ## go build（3 サービス）
	@for s in $(SERVICES); do (cd services/$$s && go build ./...) || exit 1; done

.PHONY: test
test: ## go test（3 サービス）
	@for s in $(SERVICES); do (cd services/$$s && go test ./...) || exit 1; done

# ---- 起動 ----
# SKIP_SEED=1 make up でシード投入を飛ばせる
SKIP_SEED ?=

.PHONY: up
up: gen-keys ## 開発環境を起動してシードを投入（Envoy なし。:8083 と :3002）
	docker compose up -d --build
	@if [ -z "$(SKIP_SEED)" ]; then $(MAKE) seed; else echo "SKIP_SEED=1 のためシードを飛ばしました"; fi

.PHONY: up-proxy
up-proxy: gen-keys ## Envoy 込みで起動してシードを投入（:8080 に統一）
	docker compose --profile proxy up -d --build
	@if [ -z "$(SKIP_SEED)" ]; then \
		AUTH_BASE_URL=http://localhost:8080 GRAPHQL_URL=http://localhost:8080/graphql $(MAKE) seed; \
	else echo "SKIP_SEED=1 のためシードを飛ばしました"; fi

.PHONY: seed
seed: ## 開発用のシードデータを投入（冪等。単体でも実行できる）
	./scripts/seed.sh

.PHONY: down
down: ## 停止してボリュームも削除
	docker compose --profile proxy down -v

.PHONY: logs
logs: ## 全サービスのログを追う
	docker compose logs -f

.PHONY: router
router: ## router.json を作り直して router を再起動
	docker compose run --rm router-compose
	docker compose up -d --force-recreate router

# ---- マイグレーション ----
# 使い方: make migrate-new SVC=todo NAME=add_priority
.PHONY: migrate-new
migrate-new: ## マイグレーションの雛形を生成（SVC= と NAME= が必要）
	@test -n "$(SVC)" || (echo "SVC=auth|user|todo を指定してください" && exit 1)
	@test -n "$(NAME)" || (echo "NAME=xxx を指定してください" && exit 1)
	docker run --rm -v $(PWD)/db/$(SVC)/migrations:/m migrate/migrate:v4.17.1 \
		create -ext sql -dir /m -seq $(NAME)
