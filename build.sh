#!/usr/bin/env bash
# panixy 构建脚本(单一入口):编译 CLI / 打离线包 / 清理产物
# 用法: build.sh [命令]
#   编译(默认)   build.sh [--arch amd64|arm64|all] [--ver V0.1.0]
#                 默认只编当前 CPU 架构;amd64 自带检测 AVX2(有→v3,无→v1)
#   打包         build.sh package [all|amd64|arm64] [--arch ...] [--ver ...] [--sub-url 订阅URL]
#                 默认只打包当前 CPU 架构;加 all(或 --arch all)打全部目标平台
#   清理         build.sh clean
#   帮助         build.sh -h|-?|--help
# 选项:
#   --arch <amd64|arm64|all>   目标架构。编译/打包默认当前平台;--arch all 打全部
#   --ver  <V0.1.0>            版本号(默认 git describe;无 git 时 V0.1.0-dev)
#   --sub-url <订阅URL>        打包时直连下载失败,经订阅节点建本地代理再下载
# 环境变量:
#   GOAMD64          amd64 CLI 编译档(默认自动检测 AVX2;可 GOAMD64=v1/v3/v4 强制)
#   ASSETS_SRC       本地资产目录(默认 /opt/panixy,存在即优先复制,断网可打包)
#   MIHOMO_VERSION   内核版本(默认运行时探测上游最新;显式指定可固定/复现)
#   MIHOMO_BOOT_BIN  引导代理内核(默认 /opt/panixy/bin/mihomo)
#   PROXY_PORT       引导代理端口(默认 33999)
# 打包流程:编译 CLI → 资产获取(本地优先/直连/订阅代理兜底)→ 订阅泄露扫描
#   → 组装 Panixy-V<ver>-<arch>.tar.gz + sha256(订阅 URL 永不进包)→ 清旧产物
set -euo pipefail
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
ROOT="$(cd "$(dirname "$SELF")" && pwd)"
cd "$ROOT"

usage() { sed -n '2,/^set -euo/p' "$SELF" | sed '$d; s/^# \{0,1\}//'; exit 0; }

# ---- 全局状态(供 package 与 EXIT trap 使用) ----
SUB_URL=""; PROXYX=""; BOOT_DIRF=""; TMP=""
PROXY_PORT="${PROXY_PORT:-33999}"
trap 'boot_proxy_stop; rm -rf "$TMP"' EXIT

host_arch() { case "$(uname -m)" in x86_64|amd64) echo amd64 ;; aarch64|arm64) echo arm64 ;; *) echo "" ;; esac; }
has_avx2()  { grep -qw avx2 /proc/cpuinfo 2>/dev/null; }
goamd64()   { if has_avx2; then echo v3; else echo v1; fi; }

# latest_gh_release 探测 GitHub 仓库最新 release 的 tag_name(运行时探测,不写死)。
# 失败返回空串且退出码 0,由调用方决定兜底策略(本地内核/显式版本/报错)。
latest_gh_release() {
  curl -fsSL --connect-timeout 8 "https://api.github.com/repos/$1/releases/latest" \
    2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1 || true
}

# ---- 编译:默认当前架构(amd64 自动检测 AVX2),--arch 可覆盖 ----
build_cmd() {
  local ARCH="" VER=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --arch) ARCH="$2"; shift 2 ;;
      --ver)  VER="$2"; shift 2 ;;
      -h|-\?|--help) usage ;;
      *) echo "未知参数: $1(查看用法: $0 -h)"; exit 1 ;;
    esac
  done
  [ -n "$ARCH" ] || ARCH="$(host_arch)"
  [ -n "$ARCH" ] || { echo "无法识别当前架构,请 --arch amd64|arm64|all 指定"; exit 1; }
  [ -n "$VER" ] || VER="$(git describe --tags 2>/dev/null || echo "")"
  [ -n "$VER" ] || VER="V0.1.0-dev"
  local commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  local ldflags="-s -w -X main.version=$VER -buildid="
  local targets="$ARCH"
  [ "$ARCH" = all ] && targets="amd64 arm64"
  mkdir -p dist
  echo "== 构建 panixy $VER (commit $commit, 架构: $targets) =="
  local arch
  for arch in $targets; do
    case "$arch" in
      amd64)
        local lvl="${GOAMD64:-$(goamd64)}"
        echo "  amd64(GOAMD64=$lvl)"
        (cd src && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64="$lvl" go build -trimpath -ldflags "$ldflags" -o ../dist/panixy-linux-amd64 ./cmd/panixy)
        ;;
      arm64)
        echo "  arm64"
        (cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$ldflags" -o ../dist/panixy-linux-arm64 ./cmd/panixy)
        ;;
    esac
  done
  for b in dist/panixy-linux-amd64 dist/panixy-linux-arm64; do
    [ -x "$b" ] && "$b" --version 2>/dev/null || true
  done
  (cd dist && sha256sum panixy-linux-* > sha256sums.txt)
  ls -la dist
  echo "== 完成: dist/panixy-linux-{amd64,arm64}(按需) =="
}

clean() {
  rm -rf dist/ Panixy-V*/
  echo "== 已清理 dist/ 与暂存目录 Panixy-*/ =="
}

# ---- 订阅引导代理:直连下载不了 GitHub 时,用订阅节点建本地代理再下 ----
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
  (cd dist && sha256sum "$pkg.tar.gz" > "$pkg.tar.gz.sha256")
  rm -rf "$pkg"   # 清暂存目录,只留 dist/ 产物
  echo "      产出: $pkg.tar.gz"
}

# cleanup_old 移除旧版本产物:dist/ 只保留本次版本,并清掉遗留的暂存目录。
# 旧版本可经 git 重编,无需在 dist/ 堆积;运行时的内核回滚由 panixy rollback 负责。
cleanup_old() {
  local f d
  for f in dist/Panixy-*.tar.gz dist/Panixy-*.tar.gz.sha256; do
    [ -e "$f" ] || continue
    [[ "$f" == dist/Panixy-"$VER"-*.tar.gz* ]] && continue
    rm -f "$f"
  done
  for d in Panixy-*; do
    [ -d "$d" ] || continue
    [[ "$d" == Panixy-"$VER"-* ]] && continue
    rm -rf "$d"
  done
}

missing_kernel() { [ -s "$TMP/mihomo-linux-$1.gz" ] || { echo "      ⚠️ 无 $1 内核(本地非本机架构且下载不可得),跳过该架构"; return 0; }; return 1; }

package_cmd() {
  local ARCH=""
  VER=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --arch) ARCH="$2"; shift 2 ;;
      --ver)  VER="$2"; shift 2 ;;
      --sub-url) SUB_URL="$2"; shift 2 ;;
      all|amd64|arm64) [ -n "$ARCH" ] && { echo "位置参数与 --arch 冲突: $1"; exit 1; }; ARCH="$1"; shift ;;
      -h|-\?|--help) usage ;;
      *) echo "未知参数: $1(查看用法: $0 -h)"; exit 1 ;;
    esac
  done
  [ -n "$ARCH" ] || ARCH="$(host_arch)"          # 打包默认当前 CPU 架构
  [ -n "$ARCH" ] || { echo "无法识别当前架构,请 --arch amd64|arm64|all 或位置参数 all 指定"; exit 1; }
  [ -n "$VER" ] || VER="$(git describe --tags 2>/dev/null || echo "V0.1.0-dev")"
  MIHOMO_VER="${MIHOMO_VERSION:-$(latest_gh_release MetaCubeX/mihomo)}"
  # 本地资产源(断网打包):存在则优先复制,缺失才联网下载
  local SRC="${ASSETS_SRC:-/opt/panixy}"
  # 联网探测失败时兜底:本地内核版本 > 明确报错(绝不静默写死)
  if [ -z "$MIHOMO_VER" ] && [ -x "$SRC/bin/mihomo" ]; then
    MIHOMO_VER="$("$SRC/bin/mihomo" -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
  fi
  [ -n "$MIHOMO_VER" ] || { echo "无法确定 mihomo 内核版本(联网探测失败且无本地内核);请用 MIHOMO_VERSION=vX.Y.Z 显式指定"; exit 1; }

  BOOT_DIRF="$(mktemp -d)/panixy-boot-proxy.dir"; : > "$BOOT_DIRF" 2>/dev/null || BOOT_DIRF=/tmp/panixy-boot-proxy.$$.dir

  echo "== [1/5] 编译(CLI, --arch $ARCH) =="
  build_cmd --arch "$ARCH" --ver "$VER"

  echo "== [2/5] 资产获取(本地优先: $SRC;缺失才下载) =="
  TMP="$(mktemp -d)"
  # geo 三件
  local geo="https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
  local f
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
  local base="https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER"
  local HA; HA="$(host_arch)"
  local KERNEL_ARCHS="$ARCH"
  [ "$ARCH" = all ] && KERNEL_ARCHS="amd64 arm64"
  local arch
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

  echo "== [4/5] 组装 =="
  case "$ARCH" in
    amd64|arm64) missing_kernel "$ARCH" || build_one "$ARCH" ;;
    all) missing_kernel amd64 || build_one amd64; missing_kernel arm64 || build_one arm64 ;;
    *) echo "--arch 只能是 amd64|arm64|all"; exit 1 ;;
  esac

  echo "== [5/5] 完成 =="
  cleanup_old
  ls -la dist/Panixy-*.tar.gz* 2>/dev/null | tail -4
}

case "${1:-}" in
  -h|-\?|--help) usage ;;
  clean)  clean ;;
  package) shift; package_cmd "$@" ;;
  *)      build_cmd "$@" ;;
esac
