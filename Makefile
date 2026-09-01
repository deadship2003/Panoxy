.PHONY: all build install uninstall test e2e test-all clean lint help _build-amd64 _build-arm64 _checksums

# panixy Makefile — 本机开发入口(编译/安装/测试/清理)
# 打包分发用 build.sh(仓库根目录);两者互不引用,各自内联 go build
# 内核已内嵌于 CLI(单二进制),测试无需外部 mihomo 二进制。

PANOXY_VERSION ?= $(shell git describe --tags 2>/dev/null || echo "V0.0.1-dev")
PROG           ?= Panoxy
PREFIX        ?= /usr/local
BINDIR        ?= $(PREFIX)/bin
DESTDIR       ?=
HOST_ARCH     := $(shell uname -m | sed -e 's/^x86_64$$/amd64/' -e 's/^aarch64$$/arm64/')
ARCH          ?= $(HOST_ARCH)
GOAMD64       ?= $(shell grep -qw avx2 /proc/cpuinfo 2>/dev/null && echo v3 || echo v1)

LDFLAGS := -s -w -X main.version=$(PANOXY_VERSION) -X github.com/deadship2003/Panoxy/internal/constants.ProgName=$(PROG) -buildid=

# 命令回显:默认静默(@ 前缀),只输出关键状态;
# 需排查构建时用 make -d(--debug)查看 make 内部调试输出,或 make -n 仅打印命令不执行。
Q := @

# 展开目标平台;ARCH 非法时立即报错(而非在 shell 里二次判断)。
ifeq ($(ARCH),all)
  BUILD_ARCHS := amd64 arm64
else ifeq ($(ARCH),amd64)
  BUILD_ARCHS := amd64
else ifeq ($(ARCH),arm64)
  BUILD_ARCHS := arm64
else
  $(error ARCH 只能是 amd64|arm64|all(当前: $(ARCH)))
endif

all: build ## 默认:编译当前平台二进制

build: $(addprefix _build-,$(BUILD_ARCHS)) _checksums ## 编译目标二进制(默认当前平台;ARCH=amd64|arm64|all 覆盖;make -d 看调试输出)

_build-amd64:
	@mkdir -p dist
	@echo "  → 编译 amd64 (GOAMD64=$(GOAMD64))"
	$(Q)cd src && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=$(GOAMD64) go build -trimpath -ldflags "$(LDFLAGS)" -o ../dist/$(PROG)-linux-amd64 ./cmd/Panoxy

_build-arm64:
	@mkdir -p dist
	@echo "  → 编译 arm64"
	$(Q)cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o ../dist/$(PROG)-linux-arm64 ./cmd/Panoxy

_checksums:
	$(Q)(cd dist && sha256sum $(PROG)-linux-* > sha256sums.txt 2>/dev/null || true)
	@echo "完成 → 产物在 dist/,校验和见 dist/sha256sums.txt"

install: build ## 安装 CLI → $(DESTDIR)$(BINDIR)/$(PROG)(PREFIX/BINDIR/DESTDIR 可覆盖)
	$(Q)install -Dm755 dist/$(PROG)-linux-$(ARCH) $(DESTDIR)$(BINDIR)/$(PROG)
	@echo "→ 已安装 $(DESTDIR)$(BINDIR)/$(PROG)"

uninstall: ## 卸载已安装的 CLI
	$(Q)rm -f $(DESTDIR)$(BINDIR)/$(PROG)
	@echo "→ 已卸载 $(DESTDIR)$(BINDIR)/$(PROG)"

test: ## 运行单元测试(进程内内核,无需外部 mihomo)
	$(Q)cd src && go test ./internal/... -count=1 -timeout 120s

e2e: ## 运行端到端测试(约 60s;自行编译 panixy 单二进制)
	$(Q)cd src && go test ./tests/ -count=1 -timeout 300s -v

test-all: test e2e ## 运行全部测试

clean: ## 清理全部编译产物(dist/ 与暂存目录)
	$(Q)rm -rf dist/ $(PROG)-V*/
	@echo "→ 已清理"

lint: ## 代码检查
	$(Q)cd src && go vet ./...

help: ## 显示所有目标(调试时用 make -d/--debug)
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
