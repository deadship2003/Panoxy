#!/usr/bin/env bash
# panixy 编译脚本:双架构静态二进制 + sha256
# 用法: scripts/build.sh [版本号]      (默认取 git describe/tag,无 git 时 0.1.0-dev)
set -euo pipefail
cd "$(dirname "$0")/.."

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
