.PHONY: build test e2e package clean help

# panixy Makefile — 一键入口(委托根目录 build.sh)
# 用法: make <target>   (make 或 make help 查看全部)

PANIXY_VERSION ?= $(shell git describe --tags 2>/dev/null || echo "V0.1.0-dev")

help: ## 显示所有目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## 编译当前架构二进制 → dist/(amd64 自动检测 AVX2;--arch 可覆盖)
	@./build.sh --ver $(PANIXY_VERSION)

test: ## 运行单元测试(需 mihomo 内核)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/panixy/bin/mihomo} go test ./internal/... -count=1 -timeout 120s

e2e: ## 运行端到端测试(约 60s)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/panixy/bin/mihomo} go test ./tests/ -count=1 -timeout 300s -v

test-all: test e2e ## 运行全部测试

package: ## 打双架构离线包 → dist/(编译全部目标平台)
	@./build.sh package --ver $(PANIXY_VERSION)

clean: ## 清理全部编译产物(dist/ 与暂存目录)
	@./build.sh clean

lint: ## 代码检查
	@cd src && go vet ./...
