.PHONY: build test e2e package clean help

# panixy Makefile — 一键入口
# 用法: make <target>   (make 或 make help 查看全部)

PANIXY_VERSION ?= $(shell git describe --tags 2>/dev/null || echo "0.1.0-dev")
GOAMD64 ?= v3

help: ## 显示所有目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## 编译双架构二进制 → dist/(GOAMD64 可覆盖,默认 v3)
	@GOAMD64=$(GOAMD64) ./scripts/build.sh $(PANIXY_VERSION)

test: ## 运行单元测试(需 mihomo 内核)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/panixy/bin/mihomo} go test ./internal/... -count=1 -timeout 120s

e2e: ## 运行端到端测试(约 60s)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/panixy/bin/mihomo} go test ./tests/ -count=1 -timeout 300s -v

test-all: test e2e ## 运行全部测试

package: ## 打离线包(默认当前架构;ASSETS_SRC 可指定本地资产)
	@./scripts/package.sh --ver $(PANIXY_VERSION)

package-all: ## 打双架构离线包
	@./scripts/package.sh --arch all --ver $(PANIXY_VERSION)

clean: ## 清理编译产物
	rm -rf dist/ Panixy-V*/

lint: ## 代码检查
	@cd src && go vet ./...
