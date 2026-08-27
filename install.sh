#!/usr/bin/env bash
# panixy 网关安装引导 — 全离线;失败自动回滚到运行前状态
# 配置来源优先级: 已存在的 /etc/clash.yaml > 包根目录手工放的 clash.yaml > assets 通用模板
set -Eeuo pipefail
[ $EUID -eq 0 ] || { echo "请用 sudo 运行"; exit 1; }
DIR=$(cd "$(dirname "$0")" && pwd)

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  A=amd64 ;;
  aarch64) A=arm64 ;;
  *) echo "不支持的架构: $ARCH (本包内置 amd64/arm64)"; exit 1 ;;
esac
command -v systemctl >/dev/null || { echo "需要 systemd"; exit 1; }
command -v curl      >/dev/null || { echo "需要 curl";  exit 1; }

ROOT="${PANIXY_ROOT:-/opt/panixy}"
CONF="${PANIXY_CONF:-/etc/clash.yaml}"
CLI="${PANIXY_CLI:-/usr/local/bin/panixy}"
UNIT_DIR="${PANIXY_UNIT_DIR:-/etc/systemd/system}"
SYSCTL="${PANIXY_SYSCTL:-/etc/sysctl.d/99-panixy.conf}"
BIN=$ROOT/bin/mihomo

# ---- 运行前状态快照(回滚依据) ----
OPT_NEW=0; CONF_NEW=0; CLI_NEW=0
[ -d "$ROOT" ] || OPT_NEW=1
[ -f "$CONF" ] || CONF_NEW=1
[ -f "$CLI" ]  || CLI_NEW=1
PREV_FWD=$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || echo 0)

rollback() {
  echo
  echo "!! 安装失败 —— 回滚到运行前状态"
  systemctl disable --now panixy.service panixy-upgrade.timer >/dev/null 2>&1 || true
  rm -f "$UNIT_DIR/panixy.service" "$UNIT_DIR/panixy-upgrade.service" "$UNIT_DIR/panixy-upgrade.timer"
  systemctl daemon-reload >/dev/null 2>&1 || true
  rm -f "$SYSCTL"
  sysctl -w net.ipv4.ip_forward="$PREV_FWD" >/dev/null 2>&1 || true
  if [ "$CONF_NEW" = 1 ]; then rm -f "$CONF"; fi
  if [ "$CLI_NEW" = 1 ];  then rm -f "$CLI";  fi
  if [ "$OPT_NEW" = 1 ]; then
    rm -rf "$ROOT"
  else
    echo "   (注意: $ROOT 原本已存在,本次新增文件保留在原地)"
  fi
  echo "!! 回滚完成,系统已恢复原状"
  exit 1
}
trap rollback ERR

mkdir -p "$ROOT/bin" "$ROOT/ui"

# 1. 内核:已存在则尊重现场不覆盖
if [ ! -x "$BIN" ]; then
  if [ "$ARCH" = x86_64 ] && ! grep -qm1 avx2 /proc/cpuinfo; then
    echo "错误: x86_64 CPU 不支持 AVX2,包内 amd64-v3 内核无法运行(解压后 exec 会 SIGILL)。"
    echo "   方案: 手动放置 compatible 版内核到 $BIN 后重跑本脚本,或换支持 AVX2 的机器。"
    exit 1
  fi
  gz=$(ls "$DIR"/assets/core/mihomo-linux-$A-*.gz 2>/dev/null | sort -V | tail -1 || true)
  if [ -z "$gz" ]; then echo "assets 缺 $A 内核"; exit 1; fi
  gzip -dc "$gz" > "$BIN" && chmod 755 "$BIN"
  echo "[1/5] 内核: $($BIN -v | head -1)"
else
  echo "[1/5] 内核已存在,保留: $($BIN -v | head -1)"
fi

# 2. geo 数据 + 分流规则(规则源在 raw.githubusercontent.com,国内直连不可达,离线预置首启即生效)
for f in GeoIP.dat GeoSite.dat Country.mmdb; do
  if [ ! -f "$ROOT/$f" ]; then cp "$DIR/assets/geo/$f" "$ROOT/$f"; fi
done
mkdir -p "$ROOT/rule_provider"
if [ -f "$DIR/assets/rule/AWAvenue-Ads.yaml" ]; then
  [ -f "$ROOT/rule_provider/AWAvenue-Ads.yaml" ] || cp "$DIR/assets/rule/AWAvenue-Ads.yaml" "$ROOT/rule_provider/"
else
  echo "   (包内未带广告规则文件,首启将由内核联网拉取)"
fi
echo "[2/5] geo 与规则数据就位"

# 3. Web 管理面板(metacubexd,之后由 panixy upgrade 自动更新)
if [ ! -d "$ROOT/ui/official" ]; then
  cp -r "$DIR/assets/ui/official" "$ROOT/ui/official"
  echo "unknown" > "$ROOT/ui/.official.version"
fi
echo "[3/5] Web UI 就位 (http://<本机IP>:9999/ui/)"

# 4. 配置:现有 > 包内手工 > 通用模板
if [ -f "$CONF" ]; then
  echo "[4/5] 检测到现有配置,保留不动: $CONF"
elif [ -f "$DIR/clash.yaml" ]; then
  cp "$DIR/clash.yaml" "$CONF"
  echo "[4/5] 采用包内手工配置: $DIR/clash.yaml -> $CONF"
else
  cp "$DIR/assets/clash-template.yaml" "$CONF"
  # 模板路径才替换随机 API 密钥(自带配置不动);CLI 运行时从 $CONF 解析密钥,无需同步
  SECRET_NEW=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
  sed -i "s/^secret: deadship/secret: $SECRET_NEW/" "$CONF"
  echo "[4/5] 写入通用模板 $CONF —— 记得 sudo panixy set-sub '<订阅URL>'"
  echo "      无外网环境可离线导入:panixy set-sub '<URL>' <下载好的订阅文件>"
  echo "      面板 API 密钥(随机生成): $SECRET_NEW  (日后查看: grep '^secret' $CONF)"
fi

# 5. CLI + 服务(panixy install 内部含预检/健康验证/失败自回滚)
install -m755 "$DIR/panixy" "$CLI"
echo "[5/5] 服务:"
"$CLI" install
if [ "$#" -gt 0 ]; then
  "$CLI" set-sub "$@"
else
  echo "提示: sudo panixy set-sub '<订阅链接>' 设置订阅"
fi

trap - ERR
echo
echo "完成。panixy status 查看健康;内核与 UI 每天 04:17 自动升级。"
echo "手工配置:改完后 panixy check 验证,panixy apply-conf <文件> 生效(失败自动恢复)。"
