#!/usr/bin/env bash
# panixy 编译脚本:静态二进制 + sha256(CGO_ENABLED=0)
# 用法: scripts/build.sh [版本号]
#   版本号缺省取 git describe/tag,无 git 时 0.1.0-dev
#   -h | -? | --help 显示本帮助
#   环境变量 GOOS/GOARCH 可覆盖(默认双架构 linux/amd64+arm64,GOAMD64=v1 兼容老 CPU)
set -euo pipefail
cd "$(dirname "$0")/.."
case "${1:-}" in
  -h|-\?|--help) sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
esac

VER="${1:-$(git describe --tags 2>/dev/null || echo "")}"
[ -n "$VER" ] || VER="0.1.0-dev"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
OUT=dist
mkdir -p "$OUT"

LDFLAGS="-s -w -X main.version=$VER -buildid="
# 注:GOAMD64=v1 保证 CLI 在任意 x86_64 上可跑(内核的 v3 指令集优化只对 mihomo 生效)

echo "== 构建 panixy $VER (commit $COMMIT) =="
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/panixy-linux-amd64" ./cmd/panixy
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/panixy-linux-arm64" ./cmd/panixy

"$OUT/panixy-linux-amd64" --version 2>/dev/null || true
(cd "$OUT" && sha256sum panixy-linux-* > sha256sums.txt)
ls -la "$OUT"
echo "== 完成: $OUT/panixy-linux-{amd64,arm64} =="
