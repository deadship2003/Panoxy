.PHONY: all build install uninstall test e2e test-all clean lint help

# panixy Makefile — 本机开发入口(编译/安装/测试/清理)
# 打包分发用 build.sh(仓库根目录);两者互不引用,各自内联 go build

PANOXY_VERSION ?= $(shell git describe --tags 2>/dev/null || echo "V0.0.1-dev")
PROG           ?= Panoxy
PREFIX        ?= /usr/local
BINDIR        ?= $(PREFIX)/bin
DESTDIR       ?=
HOST_ARCH     := $(shell uname -m | sed -e 's/^x86_64$$/amd64/' -e 's/^aarch64$$/arm64/')
ARCH          ?= $(HOST_ARCH)
GOAMD64       ?= $(shell grep -qw avx2 /proc/cpuinfo 2>/dev/null && echo v3 || echo v1)

all: build ## 默认:编译当前平台二进制

build: ## 编译目标二进制(默认当前平台,amd64 自动检测 AVX2;ARCH=amd64|arm64|all 覆盖)
	@mkdir -p dist
	@set -e; if [ "$(ARCH)" = all ]; then targets="amd64 arm64"; else targets="$(ARCH)"; fi; \
	for a in $$targets; do \
		case "$$a" in \
			amd64) lvl="$(GOAMD64)"; \
				(cd src && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64="$$lvl" go build -trimpath -ldflags "-s -w -X main.version=$(PANOXY_VERSION) -X github.com/deadship2003/Panoxy/internal/constants.ProgName=$(PROG) -buildid=" -o ../dist/$(PROG)-linux-amd64 ./cmd/panixy); \
				echo "  → dist/$(PROG)-linux-amd64 (amd64 GOAMD64=$$lvl)" ;; \
			arm64) (cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$(PANOXY_VERSION) -X github.com/deadship2003/Panoxy/internal/constants.ProgName=$(PROG) -buildid=" -o ../dist/$(PROG)-linux-arm64 ./cmd/panixy); \
				echo "  → dist/$(PROG)-linux-arm64 (arm64)" ;; \
			*) echo "ARCH 只能是 amd64|arm64|all(当前: $(ARCH))" >&2; exit 1 ;; \
		esac; \
	done
	@(cd dist && sha256sum $(PROG)-linux-* > sha256sums.txt 2>/dev/null || true)
	@echo "完成 → 产物在 dist/,校验和见 dist/sha256sums.txt"

install: build ## 安装 CLI → $(DESTDIR)$(BINDIR)/$(PROG)(PREFIX/BINDIR/DESTDIR 可覆盖)
	@install -Dm755 dist/$(PROG)-linux-$(ARCH) $(DESTDIR)$(BINDIR)/$(PROG) && echo "→ 已安装 $(DESTDIR)$(BINDIR)/$(PROG)"

uninstall: ## 卸载已安装的 CLI
	@rm -f $(DESTDIR)$(BINDIR)/$(PROG)

test: ## 运行单元测试(需 mihomo 内核)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/$(PROG)/bin/mihomo} go test ./internal/... -count=1 -timeout 120s

e2e: ## 运行端到端测试(约 60s)
	@cd src && MIHOMO_BIN=$${MIHOMO_BIN:-/opt/$(PROG)/bin/mihomo} go test ./tests/ -count=1 -timeout 300s -v

test-all: test e2e ## 运行全部测试

clean: ## 清理全部编译产物(dist/ 与暂存目录)
	@rm -rf dist/ $(PROG)-V*/

lint: ## 代码检查
	@cd src && go vet ./...

help: ## 显示所有目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
