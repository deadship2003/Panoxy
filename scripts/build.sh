#!/usr/bin/env bash
# panixy 编译脚本:静态二进制 + sha256(CGO_ENABLED=0)
# 用法: scripts/build.sh [版本号]
#   版本号缺省取 git describe/tag,无 git 时 0.1.0-dev
#   -h | -? | --help 显示本帮助
#   环境变量 GOAMD64 可覆盖(默认 v3,需 AVX2;老 CPU 设 GOAMD64=v1 全兼容)
set -euo pipefail
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
cd "$(dirname "$SELF")/../src"
case "${1:-}" in
  -h|-\?|--help) sed -n '2,/^set -euo/p' "$SELF" | sed '$d; s/^# \{0,1\}//'; exit 0 ;;
esac

VER="${1:-$(git describe --tags 2>/dev/null || echo "")}"
[ -n "$VER" ] || VER="V0.1.0-dev"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
OUT=../dist
mkdir -p "$OUT"

LDFLAGS="-s -w -X main.version=$VER -buildid="
# 注:GOAMD64 默认 v3(需 AVX2:Intel 2013+/AMD 2017+);老 CPU 设 GOAMD64=v1 全兼容;CLI 收益≈0

echo "== 构建 panixy $VER (commit $COMMIT) =="
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=${GOAMD64:-v3} go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/panixy-linux-amd64" ./cmd/panixy
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/panixy-linux-arm64" ./cmd/panixy

"$OUT/panixy-linux-amd64" --version 2>/dev/null || true
(cd "$OUT" && sha256sum panixy-linux-* > sha256sums.txt)
ls -la "$OUT"
echo "== 完成: $OUT/panixy-linux-{amd64,arm64} =="
