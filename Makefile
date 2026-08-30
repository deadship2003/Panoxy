.PHONY: all build install uninstall test e2e test-all clean lint help

# panixy Makefile — 本机开发入口(编译/安装/测试/清理)
# 打包分发用 build.sh(仓库根目录);两者互不引用,各自内联 go build

PANIXY_VERSION ?= $(shell git describe --tags 2>/dev/null || echo "V0.1.0-dev")
PREFIX        ?= /usr/local
BINDIR        ?= $(PREFIX)/bin
DESTDIR       ?=
HOST_ARCH     := $(shell uname -m | sed -e 's/^x86_64$$/amd64/' -e 's/^aarch64$$/arm64/')
ARCH          ?= $(HOST_ARCH)
GOAMD64       ?= $(shell grep -qw avx2 /proc/cpuinfo 2>/dev/null && echo v3 || echo v1)

all: build ## 默认:编译当前平台二进制

build: ## 编译当前平台二进制(amd64 自动检测 AVX2;ARCH=amd64|arm64 覆盖)→ dist/
	@mkdir -p dist && (cd src && CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) GOAMD64=$(GOAMD64) go build -trimpath -ldflags "-s -w -X main.version=$(PANIXY_VERSION) -buildid=" -o ../dist/panixy-linux-$(ARCH) ./cmd/panixy) && (cd dist && sha256sum panixy-linux-$(ARCH) > sha256sums.txt 2>/dev/null || true) && ls -la dist

install: build ## 安装 CLI → $(DESTDIR)$(BINDIR)/panixy(PREFIX/BINDIR/DESTDIR 可覆盖)
	@install -Dm755 dist/panixy-linux-$(ARCH) $(DESTDIR)$(BINDIR)/panixy && echo "→ 已安装 $(DESTDIR)$(BINDIR)/panixy"

uninstall: ## 卸载已安装的 CLI
	@rm -f $(DESTDIR)$(BINDIR)/panixy

test: ## 运行单元测试(需 mihomo 内核)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/panixy/bin/mihomo} go test ./internal/... -count=1 -timeout 120s

e2e: ## 运行端到端测试(约 60s)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/panixy/bin/mihomo} go test ./tests/ -count=1 -timeout 300s -v

test-all: test e2e ## 运行全部测试

clean: ## 清理全部编译产物(dist/ 与暂存目录)
	@rm -rf dist/ Panixy-V*/

lint: ## 代码检查
	@cd src && go vet ./...

help: ## 显示所有目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
