#!/usr/bin/env bash
# panixy 离线包打包脚本(本地与 CI 同一真源)
# 用法: scripts/package.sh [选项]
#   --arch <amd64|arm64|all>   目标架构,默认仅当前硬件平台(省时省流量)
#   --ver <V0.1.0>             版本号,默认取 git describe
#   --sub-url <订阅URL>        直连下载失败时,经订阅节点建立本地代理再下载
#   -h | -? | --help           显示本帮助
# 环境变量:ASSETS_SRC=本地资产目录(默认 /opt/panixy,存在即优先复制,断网可打包)
#          MIHOMO_VERSION=内核版本(默认运行时探测上游最新;显式指定可固定/复现)
#          MIHOMO_BOOT_BIN=引导代理内核(默认 /opt/panixy/bin/mihomo)
#          PROXY_PORT=引导代理端口(默认 33999)
# 流程:编译 CLI → 资产获取(本地优先/直连 15s 检测/订阅代理兜底)→ 订阅泄露扫描
#      → 组装 Panixy-V<ver>-<arch>.tar.gz + sha256(订阅 URL 永不进包)
set -euo pipefail
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
cd "$(dirname "$SELF")/.."
host_arch() { case "$(uname -m)" in x86_64) echo amd64 ;; aarch64) echo arm64 ;; *) echo "" ;; esac; }
has_avx2() { grep -qw avx2 /proc/cpuinfo 2>/dev/null; }   # amd64 有 AVX2 才能用 v3 内核

# latest_gh_release 探测 GitHub 仓库最新 release 的 tag_name(运行时探测,不写死)。
# 失败返回空串且退出码 0,由调用方决定兜底策略(本地内核/显式版本/报错)。
latest_gh_release() {
  curl -fsSL --connect-timeout 8 "https://api.github.com/repos/$1/releases/latest" \
    2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1 || true
}

usage() { sed -n '2,/^set -euo/p' "$SELF" | sed '$d; s/^# \{0,1\}//'; exit 0; }
ARCH=""; VER=""
while [ $# -gt 0 ]; do
  case "$1" in
    --arch) ARCH="$2"; shift 2 ;;
    --ver)  VER="$2"; shift 2 ;;
    --sub-url) SUB_URL="$2"; shift 2 ;;
    -h|-\?|--help) usage ;;
    *) echo "未知参数: $1(查看用法: $0 -h)"; exit 1 ;;
  esac
done
[ -n "$ARCH" ] || ARCH="$(host_arch)"   # 默认:仅当前硬件平台
[ -n "$ARCH" ] || { echo "无法识别当前架构,请 --arch amd64|arm64|all 指定"; exit 1; }
[ -n "$VER" ] || VER="$(git describe --tags 2>/dev/null || echo "V0.1.0-dev")"
MIHOMO_VER="${MIHOMO_VERSION:-$(latest_gh_release MetaCubeX/mihomo)}"   # 运行时探测上游最新
# 本地资产源(断网打包):存在则优先复制,缺失才联网下载
SRC="${ASSETS_SRC:-/opt/panixy}"
# 联网探测失败时兜底:本地内核版本 > 明确报错(绝不静默写死)
if [ -z "$MIHOMO_VER" ] && [ -x "$SRC/bin/mihomo" ]; then
  MIHOMO_VER="$("$SRC/bin/mihomo" -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
fi
[ -n "$MIHOMO_VER" ] || { echo "无法确定 mihomo 内核版本(联网探测失败且无本地内核);请用 MIHOMO_VERSION=vX.Y.Z 显式指定"; exit 1; }

# ---- 订阅引导代理:直连下载不了 GitHub 时,用订阅节点建本地代理再下 ----
# 用法:SUB_URL='订阅链接' scripts/package.sh ...(引导内核取 MIHOMO_BOOT_BIN,默认 /opt/panixy/bin/mihomo)
SUB_URL="${SUB_URL:-}"
PROXY_PORT="${PROXY_PORT:-33999}"
PROXYX=""
BOOT_DIRF="$(mktemp -d)/panixy-boot-proxy.dir"; : > "$BOOT_DIRF" 2>/dev/null || BOOT_DIRF=/tmp/panixy-boot-proxy.$$.dir
boot_proxy() {
  [ -n "$SUB_URL" ] || return 1
  local BOOT_BIN="${MIHOMO_BOOT_BIN:-/opt/panixy/bin/mihomo}"
  [ -x "$BOOT_BIN" ] || { echo "      ⚠️ 无引导内核($BOOT_BIN),无法经订阅下载"; return 1; }
  local d; d="$(mktemp -d)"
  cat > "$d/boot.yaml" <<YEOF
mixed-port: $PROXY_PORT
mode: rule
log-level: warning
proxy-providers:
  boot:
    type: http
    url: "$SUB_URL"
    path: ./boot.sub.yaml
    interval: 86400
    health-check: {enable: false}
proxy-groups:
  - {name: P, type: select, use: [boot]}
rules:
  - MATCH,P
YEOF
  (cd "$d" && nohup "$BOOT_BIN" -f boot.yaml -d "$d" > boot.log 2>&1 & echo $! > "$d/pid")
  local i ok=0
  for i in $(seq 1 25); do
    if curl -s -m 3 -x "http://127.0.0.1:$PROXY_PORT" -o /dev/null https://www.gstatic.com/generate_204; then ok=1; break; fi
    sleep 1
  done
  if [ "$ok" = 1 ]; then
    echo "$d" > "$BOOT_DIRF"
    echo "      已用订阅建立引导代理(127.0.0.1:$PROXY_PORT)"
    PROXYX="http://127.0.0.1:$PROXY_PORT"
    return 0
  fi
  echo "      ⚠️ 引导代理未就绪(订阅不可达?)"; kill "$(cat "$d/pid")" 2>/dev/null; rm -rf "$d"; return 1
}
boot_proxy_stop() {
  [ -s "$BOOT_DIRF" ] || return 0
  local d; d="$(cat "$BOOT_DIRF")"
  [ -f "$d/pid" ] && kill "$(cat "$d/pid")" 2>/dev/null
  rm -rf "$d"; : > "$BOOT_DIRF"
}
trap 'boot_proxy_stop; rm -rf "$TMP"' EXIT

# dl:直连优先(短超时),失败且配了 SUB_URL 则经引导代理
dl() {
  curl -fsSL --connect-timeout 6 --retry 1 -o "$1" "$2" && return 0
  [ -n "$SUB_URL" ] || return 1
  [ -z "$PROXYX" ] && { boot_proxy || return 1; }
  curl -fsSL --connect-timeout 10 --retry 2 -x "$PROXYX" -o "$1" "$2"
}

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
"$(dirname "$SELF")/build.sh" "$VER"

echo "== [2/5] 资产获取(本地优先: $SRC;缺失才下载) =="
TMP=$(mktemp -d)
# geo 三件
geo="https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
for f in GeoIP.dat GeoSite.dat Country.mmdb; do
  if [ -f "$SRC/$f" ]; then cp "$SRC/$f" "$TMP/$f"; echo "      本地: $f"
  else dl "$TMP/$f" "$geo/$(echo $f | tr 'A-Z' 'a-z' | sed 's/\.dat$/.dat/;s/country\.mmdb/country.mmdb/')" || true; fi
done
# 广告规则
if [ -f "$SRC/rule_provider/AWAvenue-Ads.yaml" ]; then cp "$SRC/rule_provider/AWAvenue-Ads.yaml" "$TMP/"; echo "      本地: AWAvenue-Ads.yaml"
else dl "$TMP/AWAvenue-Ads.yaml" "https://raw.githubusercontent.com/TG-Twilight/AWAvenue-Ads-Rule/refs/heads/main/Filters/AWAvenue-Ads-Rule-Clash-Classical.yaml" || true; fi
# 面板
if [ -d "$SRC/ui/official" ] && [ -f "$SRC/ui/official/index.html" ]; then
  (cd "$SRC/ui/official" && tar czf "$TMP/ui.tgz" .); echo "      本地: metacubexd UI"
else dl "$TMP/ui.tgz" "https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz" || true; fi
# 内核:本机架构可来自本地二进制(gzip),其余架构需下载
base="https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER"
HA=$(host_arch)
KERNEL_ARCHS="$ARCH"
[ "$ARCH" = all ] && KERNEL_ARCHS="amd64 arm64"
for arch in $KERNEL_ARCHS; do
  if [ "$arch" = "$HA" ] && [ -x "$SRC/bin/mihomo" ]; then
    gzip -c "$SRC/bin/mihomo" > "$TMP/mihomo-linux-$arch.gz"; echo "      本地: mihomo 内核($arch)"
  elif [ "$arch" = amd64 ]; then
    if has_avx2; then
      echo "      下载: mihomo 内核(amd64-v3,本机 AVX2)"; dl "$TMP/mihomo-linux-amd64.gz" "$base/mihomo-linux-amd64-v3-$MIHOMO_VER.gz" || true
    else
      echo "      下载: mihomo 内核(amd64 标准,本机无 AVX2)"; dl "$TMP/mihomo-linux-amd64.gz" "$base/mihomo-linux-amd64-$MIHOMO_VER.gz" || true
    fi
  else
    echo "      下载: mihomo 内核(arm64)"; dl "$TMP/mihomo-linux-arm64.gz" "$base/mihomo-linux-arm64-$MIHOMO_VER.gz" || true
  fi
done
[ -s "$TMP/Country.mmdb" ] && [ -s "$TMP/AWAvenue-Ads.yaml" ] && [ -s "$TMP/ui.tgz" ] || { echo "geo/规则/UI 资产不完整(本地与网络均不可得)"; exit 1; }

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
  mkdir -p dist && tar -czf "dist/$pkg.tar.gz" "$pkg"
  (cd . && (cd dist && sha256sum "$pkg.tar.gz" > "$pkg.tar.gz.sha256"))
  echo "      产出: $pkg.tar.gz"
}

echo "== [4/5] 组装 =="
missing_kernel() { [ -s "$TMP/mihomo-linux-$1.gz" ] || { echo "      ⚠️ 无 $1 内核(本地非本机架构且下载不可得),跳过该架构"; return 0; }; return 1; }
case "$ARCH" in
  amd64|arm64) missing_kernel "$ARCH" || build_one "$ARCH" ;;
  all) missing_kernel amd64 || build_one amd64; missing_kernel arm64 || build_one arm64 ;;
  *) echo "--arch 只能是 amd64|arm64|all"; exit 1 ;;
esac

echo "== [5/5] 完成 =="
ls -la dist/Panixy-*.tar.gz* 2>/dev/null | tail -4
