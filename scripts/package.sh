#!/usr/bin/env bash
# panixy 离线包打包脚本(本地与 CI 同一真源):
#   编译双架构 CLI → 下载目标平台 mihomo 内核/geo(含 Country.mmdb)/metacubexd/广告规则
#   → 订阅泄露扫描 → 组装 Panixy-V<ver>-<arch>.tar.gz + sha256
# 用法: scripts/package.sh [--arch amd64|arm64|all] [--ver V]
# 订阅 URL 处理:包内配置一律 SUB_URL_PLACEHOLDER 占位 + 打包前泄露扫描;
# 真实订阅由部署时 panixy set-sub 导入,绝不进包
set -euo pipefail
cd "$(dirname "$0")/.."

ARCH=all; VER=""
while [ $# -gt 0 ]; do
  case "$1" in
    --arch) ARCH="$2"; shift 2 ;;
    --ver)  VER="$2"; shift 2 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done
[ -n "$VER" ] || VER="$(git describe --tags 2>/dev/null || echo "V0.1.0-dev")"
MIHOMO_VER="${MIHOMO_VERSION:-v1.19.30}"   # 升级内核时同步改这里/环境变量

# ---- 订阅泄露扫描:任何真实订阅特征都不得进入公开包 ----
leak_scan() {
  local dir="$1"
  if grep -rInE 'token=[A-Za-z0-9]{8,}|SUB_URL_PLACEHOLDER.*http|/subscribe\?|client/subscribe' "$dir" \
     --include='*.yaml' --include='*.tpl' --include='*.yml' 2>/dev/null | grep -v 'SUB_URL_PLACEHOLDER'; then
    echo "!! 检测到疑似真实订阅链接,中止打包(防止私人订阅进入公开 Release)" >&2
    exit 1
  fi
}

echo "== [1/5] 编译(scripts/build.sh) =="
./scripts/build.sh "${VER#V}"

echo "== [2/5] 下载资产(内核 $MIHOMO_VER / geo / UI / 广告规则) =="
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
base="https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER"
curl -fsSL --retry 3 -o "$TMP/mihomo-linux-amd64.gz"    "$base/mihomo-linux-amd64-v3-$MIHOMO_VER.gz"
curl -fsSL --retry 3 -o "$TMP/mihomo-linux-arm64.gz"    "$base/mihomo-linux-arm64-$MIHOMO_VER.gz"
geo="https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
curl -fsSL --retry 3 -o "$TMP/GeoIP.dat"    "$geo/geoip.dat"
curl -fsSL --retry 3 -o "$TMP/GeoSite.dat"  "$geo/geosite.dat"
curl -fsSL --retry 3 -o "$TMP/Country.mmdb" "$geo/country.mmdb"     # ★ 按需打包 country.mmdb
curl -fsSL --retry 3 -o "$TMP/ui.tgz" "https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz"
curl -fsSL --retry 3 -o "$TMP/AWAvenue-Ads.yaml" \
  "https://raw.githubusercontent.com/TG-Twilight/AWAvenue-Ads-Rule/refs/heads/main/Filters/AWAvenue-Ads-Rule-Clash-Classical.yaml"
[ -s "$TMP/Country.mmdb" ] && [ -s "$TMP/AWAvenue-Ads.yaml" ] || { echo "资产下载不完整"; exit 1; }

echo "== [3/5] 订阅泄露扫描 =="
leak_scan .

build_one() {
  local arch="$1"
  local pkg="Panixy-${VER}-${arch}"
  rm -rf "$pkg"; mkdir -p "$pkg/assets/core" "$pkg/assets/geo" "$pkg/assets/ui/official" "$pkg/assets/rule"
  cp "dist/panixy-linux-$arch" "$pkg/panixy"; chmod +x "$pkg/panixy"
  cp "$TMP/mihomo-linux-$arch.gz" "$pkg/assets/core/mihomo-linux-$arch-$MIHOMO_VER.gz"
  cp "$TMP"/GeoIP.dat "$TMP"/GeoSite.dat "$TMP"/Country.mmdb "$pkg/assets/geo/"
  tar xzf "$TMP/ui.tgz" -C "$pkg/assets/ui/official"
  test -f "$pkg/assets/ui/official/index.html" || { echo "UI 包异常"; exit 1; }
  cp "$TMP/AWAvenue-Ads.yaml" "$pkg/assets/rule/"
  cp README.md "$pkg/"
  leak_scan "$pkg"
  tar -czf "$pkg.tar.gz" "$pkg"
  (cd . && sha256sum "$pkg.tar.gz" > "$pkg.tar.gz.sha256")
  echo "      产出: $pkg.tar.gz"
}

echo "== [4/5] 组装 =="
case "$ARCH" in
  amd64|arm64) build_one "$ARCH" ;;
  all) build_one amd64; build_one arm64 ;;
  *) echo "--arch 只能是 amd64|arm64|all"; exit 1 ;;
esac

echo "== [5/5] 完成 =="
ls -la Panixy-*.tar.gz* 2>/dev/null | tail -4
