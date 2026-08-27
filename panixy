#!/usr/bin/env bash
# panixy — 基于 mihomo 的 Linux 透明代理部署/管理工具 (单文件,自包含:安装引导也内置)
# 取代已归档的 TPClash: 进程守护 + systemd-resolved 接管 + 内核/UI 自动升级(带健康回滚)
# 布局: /opt/panixy/bin/mihomo(内核)  /opt/panixy/ui/official(面板,自动更新)
#       /opt/panixy/(mihomo 家目录: cache.db/providers/geo)
# 用法: panixy {deploy|install|upgrade|update-ui|set-sub|check|apply-conf|status|rollback|uninstall|units|log|man}
#       详细说明: panixy --help / man panixy(deploy 后可用)
# 联动参数(secret/mixed-port/external-controller)运行时从 $CONF 解析,唯一事实源是配置文件
set -u

VER_TAG=0.0.2
ROOT="${PANIXY_ROOT:-/opt/panixy}"
BIN=$ROOT/bin/mihomo
UI_DIR=$ROOT/ui/official
UI_STAMP=$ROOT/ui/.official.version
CONF="${PANIXY_CONF:-/etc/clash.yaml}"
UNIT_DIR="${PANIXY_UNIT_DIR:-/etc/systemd/system}"
CLI="${PANIXY_CLI:-/usr/local/bin/panixy}"
MAN_GZ="${PANIXY_MAN:-/usr/local/share/man/man1/panixy.1.gz}"
KEEP=3                             # 内核备份保留份数
SYSCTL_FILE="${PANIXY_SYSCTL:-/etc/sysctl.d/99-panixy.conf}"
LASTUP=$ROOT/.last-upgrade         # 最近一次升级成功时间戳(status 展示,停滞可见)

# ---- 联动参数:配置文件 > 默认值(环境变量可覆盖,便于离线测试)----
conf_val() {  # $1=顶层键名  输出其值(去引号);文件缺失输出空
  [ -f "$CONF" ] || return 0
  awk -v k="$1" 'index($0, k":")==1 {sub("^" k ":[ ]*", ""); gsub(/^"|"$/, ""); print; exit}' "$CONF" 2>/dev/null
}
SECRET="${PANIXY_SECRET:-$(conf_val secret)}";       SECRET="${SECRET:-deadship}"
MIXED_PORT="${PANIXY_PROXY_PORT:-$(conf_val mixed-port)}"; MIXED_PORT="${MIXED_PORT:-33833}"
CTRL="$(conf_val external-controller)"
API_PORT="${PANIXY_API_PORT:-${CTRL##*:}}";          API_PORT="${API_PORT:-9999}"
PROXY="${PANIXY_PROXY:-http://127.0.0.1:$MIXED_PORT}"
API="${PANIXY_API:-http://127.0.0.1:$API_PORT}"

log() { echo "[panixy] $(date '+%F %T') $*"; }
die() { log "错误: $*"; exit 1; }
need_root() { [ $EUID -eq 0 ] || die "请用 sudo 运行"; }

lock() {  # 互斥锁:防止 timer 自动升级与手工 upgrade/rollback/set-sub 并发互踩;进程内幂等
  # 注意:exec 的重定向会永久生效,必须用 { } 限定 2>/dev/null 的作用域,
  # 否则整个脚本后续的 stderr(内核报错/curl 错误)都被吞掉
  [ -n "${PANIXY_LOCKED:-}" ] && return 0
  command -v flock >/dev/null 2>&1 || { PANIXY_LOCKED=1; return 0; }
  local f="${PANIXY_LOCK:-/run/panixy.lock}"
  if { exec 9>"$f"; } 2>/dev/null; then
    flock -n 9 || die "另一个 panixy 实例正在运行,请稍后再试"
  fi
  PANIXY_LOCKED=1
}

api_ver_now() {  # API 上报的内核版本(空=API 不可用)
  curl -s -m 3 -H "Authorization: Bearer $SECRET" "$API/version" 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+'
}
wait_healthy() {  # $1=最长等待秒(默认30) $2=期望版本(可选);轮询代替固定 sleep,慢网关不误判
  local end=$(( $(date +%s) + ${1:-30} )) v
  while [ "$(date +%s)" -lt "$end" ]; do
    v=$(api_ver_now || true)
    if [ -n "$v" ] && systemctl is-active --quiet panixy.service 2>/dev/null \
       && { [ -z "${2:-}" ] || [ "$v" = "$2" ]; }; then
      return 0
    fi
    sleep 2
  done
  return 1
}
egress_ok() {  # 经代理出网 204,重试 $1 次(默认3)
  local i code
  for i in $(seq 1 "${1:-3}"); do
    code=$(curl -s -m 8 -x "$PROXY" -o /dev/null -w '%{http_code}' https://www.gstatic.com/generate_204 2>/dev/null)
    [ "$code" = "204" ] && return 0
    sleep 3
  done
  return 1
}

# ---- 订阅导入辅助 ----
# 关键事实(实测 mihomo v1.19.x):订阅拉不到时内核照常运行、API 照常应答(0 节点),
# 且热重载不重建 provider(换 URL 不生效)——所以必须:CLI 预取订阅→预置缓存→重启→验证节点数。
sub_name() {  # 配置里第一个 proxy-provider 名(模板为 SUB;自定义配置自动识别),空=无 provider
  [ -f "$CONF" ] || return 0
  awk '/^proxy-providers:/{f=1;next} f && /^  [^ #]/{sub(/:.*/,"",$1); gsub(/"/,"",$1); print $1; exit}' "$CONF" 2>/dev/null
}
sub_ua() {  # 与内核拉订阅一致的 UA(机场按 UA 返回 Clash 格式)
  echo "clash.meta/$("$BIN" -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
}
fetch_sub() {  # $1=url $2=输出文件:直连优先,失败经本机代理(换被墙订阅时旧节点还能用);失败返回非0
  local ua; ua=$(sub_ua)
  curl -fsSL -m 20 -A "$ua" -o "$2" "$1" 2>/dev/null && return 0
  curl -fsSL -m 20 -A "$ua" -x "$PROXY" -o "$2" "$1" 2>/dev/null
}
sub_yaml_ok() {  # $1=文件:是否为含节点的 Clash YAML(机场对无效 token 常返回网页/空,须拦下)
  [ -s "$1" ] || return 1
  grep -qE '^[[:space:]]*proxies:' "$1" \
    && grep -qE '^[[:space:]]*-[[:space:]]*(\{|"?name"?:)' "$1"
}
sub_cache_path() {  # 第一个 provider 的缓存落盘位:取其 path 字段(相对 $ROOT),缺省按名兜底
  local n p
  n=$(sub_name); n=${n:-SUB}
  p=$(awk -v n="$n" 'index($0,"  " n ":")==1{f=1;next} f&&/^    path:/{sub(/^    path:[ ]*/,"");gsub(/^"|"$/,"");print;exit}' "$CONF" 2>/dev/null)
  case "$p" in
    /*) echo "$p" ;;
    '') echo "$ROOT/proxies/$n.yaml" ;;
    *)  echo "$ROOT/${p#./}" ;;
  esac
}
provider_nodes() {  # $1=provider名(默认取配置首个);已加载节点数(0=未加载/不可达/名字无法拼API路径)
  local n="${1:-$(sub_name)}"
  [ -n "$n" ] || { echo 0; return; }
  case "$n" in *[!A-Za-z0-9_.-]*) echo 0; return ;; esac
  curl -s -m 4 -H "Authorization: Bearer $SECRET" "$API/providers/proxies/$n" 2>/dev/null \
    | grep -o '"provider-name":"[^"]*"' | wc -l
}
sub_recover() {  # $1=缓存路径(空=只恢复配置) $2=非空则重启服务;set-sub 失败路径恢复原状
  mv -f "$CONF.panixy-bak" "$CONF" 2>/dev/null
  [ -n "$1" ] && [ -f "$1.panixy-bak" ] && mv -f "$1.panixy-bak" "$1"
  [ -n "${2:-}" ] && systemctl restart panixy.service >/dev/null 2>&1
}
reload_conf() {  # 热重载配置(不重启、不重建 TUN,网关链路无损);失败返回非0
  curl -fsS -m 10 -X PUT -H "Authorization: Bearer $SECRET" -H 'Content-Type: application/json' \
       -d "{\"path\": \"$CONF\"}" "$API/configs?force=0" >/dev/null 2>&1
}

gh_latest() {  # $1=repo  输出最新稳定版 tag(-f:限流403/404 判失败,自然退直连)
  curl -fsS -m 15 --retry 2 -x "$PROXY" "https://api.github.com/repos/$1/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name": *"\(v[0-9.]*\)".*/\1/p' | head -1
}
gh_latest_direct() {
  curl -fsS -m 15 --retry 2 "https://api.github.com/repos/$1/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name": *"\(v[0-9.]*\)".*/\1/p' | head -1
}
dl() {  # $1=输出 $2=url  经代理下载,失败退直连(-f:404页面不再当成功保存)
  curl -fsSL -m "${TMO:-300}" --retry 2 -x "$PROXY" -o "$1" "$2" 2>/dev/null \
    || curl -fsSL -m "${TMO:-300}" --retry 2 -o "$1" "$2" 2>/dev/null
}

write_units() {
  local d="${1:-$UNIT_DIR}"
cat > "$d/panixy.service" <<'EOF'
[Unit]
Description=panixy - mihomo transparent proxy (TUN)
After=network-online.target systemd-resolved.service
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStartPre=/opt/panixy/bin/mihomo -t -f /etc/clash.yaml -d /opt/panixy
ExecStart=/opt/panixy/bin/mihomo -f /etc/clash.yaml -d /opt/panixy -ext-ui /opt/panixy/ui/official
ExecStartPost=/bin/sh -c 'for i in 1 2 3 4 5 6 7 8 9 10; do resolvectl dns Meta 198.18.0.2 fdfe:dcba:9876::2 2>/dev/null && break; sleep 2; done; resolvectl domain Meta "~." 2>/dev/null || true'
ExecStopPost=/bin/sh -c 'resolvectl revert Meta 2>/dev/null || true; resolvectl flush-caches 2>/dev/null || true'
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
cat > "$d/panixy-upgrade.service" <<'EOF'
[Unit]
Description=panixy core & UI auto-upgrade (oneshot)

[Service]
Type=oneshot
ExecStart=/usr/local/bin/panixy upgrade
EOF
cat > "$d/panixy-upgrade.timer" <<'EOF'
[Unit]
Description=Daily panixy upgrade check at 04:17 (+0-25min jitter)

[Timer]
OnCalendar=*-*-* 04:17:00
RandomizedDelaySec=25m
Persistent=true

[Install]
WantedBy=timers.target
EOF
}

install_rollback() {  # $1=改动前的 ip_forward 值;install 失败时恢复系统原状
  systemctl disable --now panixy.service panixy-upgrade.timer >/dev/null 2>&1
  rm -f "$UNIT_DIR/panixy.service" "$UNIT_DIR/panixy-upgrade.service" "$UNIT_DIR/panixy-upgrade.timer"
  systemctl daemon-reload >/dev/null 2>&1
  rm -f "$SYSCTL_FILE"
  sysctl -w net.ipv4.ip_forward="${1:-0}" >/dev/null 2>&1
  log "已回滚:unit/timer/sysctl 移除,ip_forward 恢复为 ${1:-0}"
}

cmd_install() {
  need_root; lock
  [ -x "$BIN" ]  || die "内核不存在: $BIN (在离线包内用 panixy deploy,或手动放置)"
  "$BIN" -v >/dev/null 2>&1 || die "内核无法执行: $BIN (空文件/架构不符?重新放置)"
  [ -f "$CONF" ] || die "配置不存在: $CONF"
  mkdir -p "$ROOT/ui"
  [ -d "$UI_DIR" ] || die "UI 不存在: $UI_DIR (在离线包内用 panixy deploy)"
  [ -f "$UI_STAMP" ] || echo unknown > "$UI_STAMP"

  # 预检:配置必须过内核校验 —— 此时尚未对系统做任何改动
  "$BIN" -t -f "$CONF" -d "$ROOT" >/dev/null 2>&1 \
    || die "配置校验未通过($CONF),系统未做任何改动"

  # 记录改动前状态
  prev_fwd=$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || echo 0)

  write_units
  systemctl daemon-reload
  echo "net.ipv4.ip_forward = 1" > "$SYSCTL_FILE"
  sysctl -w net.ipv4.ip_forward=1 >/dev/null
  if ! systemctl enable --now panixy.service >/dev/null 2>&1; then
    install_rollback "$prev_fwd"
    die "服务启动失败,已回滚到运行前状态"
  fi
  if ! systemctl enable --now panixy-upgrade.timer >/dev/null 2>&1; then
    install_rollback "$prev_fwd"
    die "升级 timer 启用失败,已回滚到运行前状态"
  fi

  # 健康验证:服务 + API + TUN 三要素,轮询最多 30s
  if ! wait_healthy 30 || ! ip link show Meta >/dev/null 2>&1; then
    install_rollback "$prev_fwd"
    die "健康验证超时(服务/TUN/API 未就绪),已回滚到运行前状态"
  fi
  log "install 完成 v$VER_TAG(健康验证通过)"
  cmd_status
}

upgrade_core() {
  cur=$("$BIN" -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)
  [ -n "$cur" ] || die "无法读取当前内核版本"

  latest=$(gh_latest MetaCubeX/mihomo); [ -z "$latest" ] && latest=$(gh_latest_direct MetaCubeX/mihomo)
  [ -z "$latest" ] && { log "GitHub API 不可达,内核本次跳过"; return 0; }
  [ "$cur" = "$latest" ] && { log "内核已是最新 $cur"; return 0; }
  log "内核升级: $cur -> $latest"

  # 资产选择:V3 指令集优先(amd64;arm64 上游无 v3 变体),下载后试运行验证,失败自动降级
  local cands
  case "$(uname -m)" in
    x86_64)
      if grep -qm1 avx2 /proc/cpuinfo; then
        cands="mihomo-linux-amd64-v3-$latest mihomo-linux-amd64-$latest mihomo-linux-amd64-compatible-$latest"
      else
        cands="mihomo-linux-amd64-$latest mihomo-linux-amd64-compatible-$latest"
      fi ;;
    aarch64)
      cands="mihomo-linux-arm64-$latest mihomo-linux-arm64-compatible-$latest" ;;
    *)
      cands="mihomo-linux-amd64-compatible-$latest" ;;
  esac

  tmp=$(mktemp -d)
  local got=""
  for base in $cands; do
    log "尝试资产 ${base}.gz"
    if curl -fsSL -m 300 --retry 2 -x "$PROXY" -o "$tmp/core.gz" "https://github.com/MetaCubeX/mihomo/releases/download/$latest/${base}.gz" 2>/dev/null \
       || curl -fsSL -m 300 --retry 2 -o "$tmp/core.gz" "https://github.com/MetaCubeX/mihomo/releases/download/$latest/${base}.gz" 2>/dev/null; then
      if gzip -dc "$tmp/core.gz" > "$tmp/core" 2>/dev/null && chmod +x "$tmp/core" \
         && [ "$("$tmp/core" -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)" = "$latest" ]; then
        got=1; break
      fi
      log "资产 ${base} 无法运行(指令集不兼容?),降级下一档"
    else
      log "资产 ${base} 下载失败(404/网络),降级下一档"
    fi
  done
  [ -n "$got" ] || { log "所有候选资产均失败"; rm -rf "$tmp"; return 1; }

  bak="$BIN.bak-$cur"
  cp "$BIN" "$bak"
  install -m755 "$tmp/core" "$BIN.new" && mv -f "$BIN.new" "$BIN"
  rm -rf "$tmp"
  systemctl restart panixy.service
  # 轮询等待(最多90s)再判定,慢网关/大订阅不会被固定 sleep 误杀
  if wait_healthy 90 "$latest" && egress_ok 3; then
    log "内核升级成功 $cur -> $latest (备份 $bak)"
    ls -1t "$BIN".bak-* 2>/dev/null | tail -n +$((KEEP+1)) | xargs -r rm -f
    return 0
  else
    log "内核健康检查失败(api=$(api_ver_now) 期望 $latest),回滚到 $cur"
    mv -f "$bak" "$BIN"
    systemctl restart panixy.service
    wait_healthy 30 || log "警告:回滚后健康检查仍未通过,请 panixy log 排查"
    return 1
  fi
}

update_ui() {  # metacubexd 面板自动更新(版本化 release,与内核升级同构)
  local cur latest tmp
  cur=$(cat "$UI_STAMP" 2>/dev/null || echo none)
  latest=$(gh_latest MetaCubeX/metacubexd); [ -z "$latest" ] && latest=$(gh_latest_direct MetaCubeX/metacubexd)
  [ -z "$latest" ] && { log "UI 版本查询失败,本次跳过"; return 0; }
  [ "$cur" = "$latest" ] && { log "UI 已是最新 $cur"; return 0; }
  log "UI 升级: $cur -> $latest (metacubexd)"

  tmp=$(mktemp -d)
  TMO=120 dl "$tmp/dist.tgz" "https://github.com/MetaCubeX/metacubexd/releases/download/$latest/compressed-dist.tgz"
  mkdir -p "$tmp/x"
  tar xzf "$tmp/dist.tgz" -C "$tmp/x" 2>/dev/null \
    || { log "UI 下载/解包失败"; rm -rf "$tmp"; return 1; }
  [ -f "$tmp/x/index.html" ] || { log "UI 包异常(无 index.html)"; rm -rf "$tmp"; return 1; }

  rm -rf "$UI_DIR.old"
  [ -d "$UI_DIR" ] && mv "$UI_DIR" "$UI_DIR.old"
  mv "$tmp/x" "$UI_DIR"
  echo "$latest" > "$UI_STAMP"
  rm -rf "$tmp"

  code=$(curl -s -m 8 -o /dev/null -w '%{http_code}' "$API/ui/")
  if [ "$code" = "200" ]; then
    rm -rf "$UI_DIR.old"
    log "UI 升级成功 -> $latest (http $code)"
    return 0
  else
    log "UI 健康检查异常(http=$code),恢复旧版"
    rm -rf "$UI_DIR" && mv "$UI_DIR.old" "$UI_DIR"
    return 1
  fi
}

cmd_upgrade() {   # timer 入口: 内核 + UI;全成功才更新时间戳(status 里停滞可见)
  need_root; lock
  rc=0
  upgrade_core || rc=1
  update_ui    || rc=1
  [ "$rc" -eq 0 ] && date '+%F %T' > "$LASTUP"
  return $rc
}

cmd_set_sub() {  # 用法: panixy set-sub [订阅URL] [本地订阅文件] — 无 URL 时交互粘贴(免引号,免疫 & ? 等字符)
  # 无外网环境要点:订阅内容预取(直连→经本机代理两级,或直接给本地文件)→ 预置 provider 缓存
  # → 重启重建 provider(热重载对换 URL 无效)→ 以节点数>0 作为成功标准,绝不假成功
  need_root; lock
  local url="${1:-}" seed="${2:-}" cache tmp n i blk
  if [ -z "$url" ]; then  # 未给 URL:进入粘贴模式(read 读整行,不经 shell 解析)
    # stdin 是终端→提示后读;是管道→直接读(支持 echo URL | panixy set-sub)
    if [ -t 0 ]; then read -r -p "请粘贴订阅链接(整行粘贴后回车,无需加引号): " url
    else read -r url; fi
    [ -n "$url" ] || die "用法: panixy set-sub <订阅URL> [本地订阅文件](或无参数进入粘贴模式)"
  fi
  case "$url" in http://*|https://*) ;; *) die "URL 需以 http(s):// 开头(命令行传参时记得整体加单引号)" ;; esac
  [ -f "$CONF" ] || die "配置不存在: $CONF"
  blk=$(sub_name)
  [ -n "$blk" ] || die "配置 $CONF 无 proxy-providers 段,无法写入订阅 URL(自定义配置请手工修改后 sudo panixy apply-conf)"

  # 1) 取订阅内容:本地文件 > 直连 > 经本机代理
  tmp=$(mktemp)
  if [ -n "$seed" ]; then
    [ -f "$seed" ] || { rm -f "$tmp"; die "本地订阅文件不存在: $seed"; }
    cp "$seed" "$tmp" || { rm -f "$tmp"; die "本地订阅文件读取失败: $seed"; }
    log "使用本地订阅文件: $seed (跳过联网拉取)"
  elif ! fetch_sub "$url" "$tmp"; then
    rm -f "$tmp"
    die "订阅拉取失败(直连与经本机代理均不通): $url
  提示:命令行传 URL 须整体加单引号(含 & ? 等字符会被 shell 拆掉),或直接
  sudo panixy set-sub 回车进入粘贴模式;无外网环境可离线导入(任意设备下载好订阅后
  sudo panixy set-sub '<URL>' <订阅文件>),或指定可用代理 PANIXY_PROXY=http://主机:端口"
  fi
  if ! sub_yaml_ok "$tmp"; then
    rm -f "$tmp"
    die "订阅内容不是有效的 Clash YAML(应含 proxies: 节点列表),请检查链接/token 或本地文件"
  fi

  # 2) 备份 → 写 URL(块内无 path 时顺手补上,保证缓存可预置)→ 内核校验 → 预置缓存
  cp "$CONF" "$CONF.panixy-bak"
  if awk -v n="$blk" 'index($0,"  " n ":")==1{f=1} f&&/^    path:/{found=1} END{exit !found}' "$CONF.panixy-bak"; then haspath=1; else haspath=0; fi
  awk -v u="$url" -v n="$blk" -v p="$haspath" '
    /^[^ #]/{insub=0}
    index($0,"  " n ":")==1{insub=1}
    insub && /^    url:/{print "    url: \"" u "\""; if(p=="0") print "    path: ./proxies/" n ".yaml"; next}
    {print}
  ' "$CONF.panixy-bak" > "$CONF" || { sub_recover ""; die "配置写入失败,已恢复原文件"; }
  grep -q "\"$url\"" "$CONF" || { sub_recover ""; die "写入失败(未找到 $blk 的 url 行),已恢复原文件"; }
  trc=0
  # 注意:mihomo 的日志(含 -t 报错)走 stdout,必须合并捕获,否则错误被吞
  "$BIN" -t -f "$CONF" -d "$ROOT" >"$CONF.panixy-terr" 2>&1 || trc=$?
  if [ "$trc" -ne 0 ]; then
    cp -f "$CONF" "$CONF.panixy-rej" 2>/dev/null   # 留档被拒配置,便于排查
    sub_recover ""
    die "配置校验未通过(rc=$trc $(grep -m1 -oE 'msg="[^"]*"' "$CONF.panixy-terr" 2>/dev/null)),已恢复原配置"
  fi
  rm -f "$CONF.panixy-terr"
  cache=$(sub_cache_path)
  [ -f "$cache" ] && cp "$cache" "$cache.panixy-bak"
  mkdir -p "$(dirname "$cache")"
  cp "$tmp" "$cache" && rm -f "$tmp"

  # 3) 重启重建 provider:内核从预置缓存秒级加载节点,不依赖当时的网络
  log "订阅已写入 provider $blk,缓存已预置,重启内核生效(换 URL 必须重启,热重载不重建 provider)"
  if ! systemctl restart panixy.service >/dev/null 2>&1; then
    sub_recover "$cache" restart; die "重启失败,已恢复原订阅"
  fi
  wait_healthy 30 || { sub_recover "$cache" restart; die "重启后健康检查超时,已恢复原订阅"; }

  # 4) 成功标准 = 节点真实加载(这一步是内核能转发流量的前提)
  n=0
  for i in 1 2 3 4 5; do n=$(provider_nodes "$blk" || true); [ "$n" -gt 0 ] 2>/dev/null && break; sleep 2; done
  if [ "${n:-0}" -eq 0 ] 2>/dev/null; then
    sub_recover "$cache" restart
    die "订阅未加载(节点数为 0),已恢复原订阅;排查: panixy log / panixy check"
  fi
  log "订阅($blk)加载成功:$n 个节点(测速选优由 🔃 自动选择 组负责,默认走最快节点)"
  rm -f "$CONF.panixy-bak" "$cache.panixy-bak"
  log "订阅导入完成: $url"
  cmd_status
}

cmd_check() {  # 用法: panixy check [yaml路径] — 用内核 -t 校验配置(默认当前配置;只读,免root)
  local f="${1:-$CONF}"
  [ -f "$f" ] || die "文件不存在: $f"
  "$BIN" -t -f "$f" -d "$ROOT"
}

cmd_apply_conf() {  # 用法: panixy apply-conf <yaml路径> — 部署手工调整的配置(优先热重载;失败自动恢复)
  need_root; lock
  local f="${1:-}"
  if [ -z "$f" ] || [ ! -f "$f" ]; then die "用法: panixy apply-conf <yaml路径>"; fi
  "$BIN" -t -f "$f" -d "$ROOT" >/dev/null 2>&1 \
    || die "该文件未通过内核校验,系统未做任何改动"
  [ -f "$CONF" ] && cp "$CONF" "$CONF.panixy-bak"
  if ! cp "$f" "$CONF"; then
    if [ -f "$CONF.panixy-bak" ]; then mv "$CONF.panixy-bak" "$CONF"; fi
    die "配置复制失败,已恢复"
  fi
  if reload_conf && wait_healthy 20; then
    rm -f "$CONF.panixy-bak"
    log "配置已热重载生效(未重启): $f -> $CONF"
    cmd_status
    return 0
  fi
  log "热重载未生效,改用重启方式"
  if ! systemctl restart panixy.service >/dev/null 2>&1; then
    if [ -f "$CONF.panixy-bak" ]; then
      mv "$CONF.panixy-bak" "$CONF"
      systemctl restart panixy.service >/dev/null 2>&1
      die "重启失败,已恢复原配置"
    fi
    die "重启失败(无旧配置可恢复)"
  fi
  rm -f "$CONF.panixy-bak"
  wait_healthy 30 || log "警告:重启后健康检查未通过,请 panixy log 排查"
  log "配置已生效: $f -> $CONF"
  cmd_status
}

json_esc() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }

cmd_status() {  # 用法: panixy status [-v|-q|--json] — 健康一览
  local v= q= j=
  while [ $# -gt 0 ]; do
    case "$1" in
      -v|--verbose) v=1 ;;
      -q|--quiet)   q=1 ;;
      --json)       j=1 ;;
      *) die "未知参数: $1 (用法: panixy status [-v|-q|--json])" ;;
    esac
    shift
  done
  local svc api_ver nodes egress direct has_sub nodes_txt blk
  # 探测赋值一律 || true:探测失败不是错误(在 deploy 的 set -e 子壳里尤其不能炸)
  blk=$(sub_name || true)
  svc=$(systemctl is-active panixy.service 2>/dev/null || true)
  api_ver=$(curl -s -m 4 -H "Authorization: Bearer $SECRET" "$API/version" 2>/dev/null | grep -oE '"version":"[^"]*"' | cut -d'"' -f4 || true)
  if [ -n "$blk" ]; then nodes=$(provider_nodes "$blk" || true); else nodes=-1; fi
  egress=$(curl -s -m 8 -x "$PROXY" -o /dev/null -w '%{http_code}' https://www.gstatic.com/generate_204 2>/dev/null || true)
  egress=${egress:-000}
  direct=$(curl -s -m 6 -o /dev/null -w '%{http_code}' http://connect.rom.miui.com/generate_204 2>/dev/null || true)
  direct=${direct:-000}

  # -q:静默,仅以退出码表达健康(0=健康 1=降级 2=故障),供监控/巡检
  if [ -n "$q" ]; then
    { [ "$svc" = "active" ] && [ -n "$api_ver" ]; } || exit 2
    { [ "$nodes" -gt 0 ] 2>/dev/null && [ "$egress" = "204" ]; } || exit 1
    exit 0
  fi
  # --json:单行机器可读
  if [ -n "$j" ]; then
    local n_json=null
    [ "$nodes" -ge 0 ] 2>/dev/null && n_json=$nodes
    printf '{"version":"%s","service":"%s","api":%s,"nodes":%s,"proxy_egress":"%s","direct_egress":"%s","core":"%s","ui":"%s","last_upgrade":"%s"}\n' \
      "$VER_TAG" "$svc" "$([ -n "$api_ver" ] && echo true || echo false)" "$n_json" "$egress" "$direct" \
      "$(json_esc "$("$BIN" -v 2>/dev/null | head -1)")" \
      "$(json_esc "$(cat "$UI_STAMP" 2>/dev/null || echo unknown)")" \
      "$(json_esc "$(cat "$LASTUP" 2>/dev/null || echo "")")"
    return 0
  fi

  if [ "$nodes" -ge 0 ] 2>/dev/null; then
    nodes_txt="$nodes 个"
    [ "$nodes" = "0" ] && nodes_txt="0 ⚠️ 订阅未加载(重新导入: sudo panixy set-sub,仍失败查 panixy log)"
  else
    nodes_txt="-(无订阅 provider)"
  fi
  echo "== panixy v$VER_TAG  ($ROOT) =="
  if grep -q 'url:.*SUB_URL_PLACEHOLDER' "$CONF" 2>/dev/null; then
    echo "订阅:     ⚠️ 尚未设置! 用 sudo panixy set-sub(可回车进入粘贴模式)"
  elif [ -n "$blk" ]; then
    echo "订阅:     $(awk -v n="$blk" 'index($0,"  " n ":")==1{f=1} f&&/^    url:/{sub(/^    url:[ ]*/,"");gsub(/^"|"$/,"");print;exit}' "$CONF" 2>/dev/null | cut -c1-60)..."
  else
    echo "订阅:     -(配置无 proxy-providers 段)"
  fi
  echo "节点:     $nodes_txt"
  echo "服务:     $svc"
  echo "升级定时: $(systemctl is-active panixy-upgrade.timer 2>/dev/null) 下次: $(systemctl list-timers panixy-upgrade.timer --no-pager 2>/dev/null | awk 'NR==2{print $1,$2,$3}')"
  echo "上次升级: $(cat "$LASTUP" 2>/dev/null || echo 未知)(仅全成功时更新;过旧=升级停滞,查 panixy log)"
  echo "内核:     $($BIN -v 2>/dev/null | head -1)"
  echo "UI:       metacubexd $(cat "$UI_STAMP" 2>/dev/null || echo 未知)"
  echo "API:      ${api_ver:-不可达}"
  echo "resolved: $(resolvectl dns Meta 2>/dev/null | tail -1)"
  echo "代理出网: $egress (期望204)"
  echo "直连出网: $direct (期望204)"
  echo "内核备份: $(ls -1 "$BIN".bak-* 2>/dev/null | tr '\n' ' ')"
  if [ -n "$v" ]; then
    echo "-- 详细(-v) --"
    echo "TUN:      $(ip -brief addr show Meta 2>/dev/null | awk '{print $2,$3}' | tr '\n' ' ')"
    echo "默认路由: $(ip route show default 2>/dev/null | head -1)"
    echo "订阅缓存: $(ls -l --time-style=+%F\ %T "$(sub_cache_path)" 2>/dev/null | awk '{print $6,$7,$8}')"
    echo "规则缓存: $(ls -l --time-style=+%F\ %T "$ROOT/rule_provider/AWAvenue-Ads.yaml" 2>/dev/null | awk '{print $6,$7,$8}')"
    echo "面板:     $API/ui/ (http $(curl -s -m 5 -o /dev/null -w '%{http_code}' "$API/ui/" 2>/dev/null))"
  fi
}

cmd_rollback() {
  need_root; lock
  baks=$(ls -1t "$BIN".bak-* 2>/dev/null) || die "没有可用备份"
  if [ -n "${1:-}" ]; then bak="$BIN.bak-$1"; else bak=$(echo "$baks" | head -1); fi
  [ -f "$bak" ] || die "备份不存在: $bak (现有: $(echo "$baks" | tr '\n' ' '))"
  cur=$("$BIN" -v | grep -oE 'v[0-9.]+' | head -1)
  log "回滚 $cur -> $(basename "$bak")"
  cp "$BIN" "$BIN.bak-$cur"
  mv -f "$bak" "$BIN"
  systemctl restart panixy.service
  wait_healthy 30 || log "警告:回滚后健康检查未通过,请 panixy log 排查"
  cmd_status
}

cmd_uninstall() {
  need_root; lock
  systemctl disable --now panixy.service panixy-upgrade.timer 2>/dev/null
  rm -f "$UNIT_DIR/panixy.service" "$UNIT_DIR/panixy-upgrade.service" "$UNIT_DIR/panixy-upgrade.timer" "$SYSCTL_FILE" "$MAN_GZ"
  systemctl daemon-reload
  log "已卸载 unit/timer/sysctl/手册;数据目录 $ROOT 与 $CONF 保留(CLI 本身保留)"
}

cmd_units() {  # 输出内嵌 unit 内容(用于离线校验,不动系统)
  local t; t=$(mktemp -d); write_units "$t"
  for f in panixy.service panixy-upgrade.service panixy-upgrade.timer; do
    echo "===== $f ====="; cat "$t/$f"
  done
  rm -rf "$t"
}

cmd_log() {  # 用法: panixy log [行数] — 服务与自动升级日志
  journalctl -u panixy.service -u panixy-upgrade.service -n "${1:-80}" --no-pager
}

usage() {
cat <<EOF
panixy v$VER_TAG — 基于 mihomo 的 Linux 透明代理网关部署/管理工具

引导(在解压的离线包根目录运行):
  deploy [订阅URL] [本地订阅文件]   全新部署:内核/geo/面板/规则/配置就位 + 服务拉起
                                    (失败全量回滚;可顺带导入订阅)
  install                           仅部署 systemd 服务(文件已就位时)

日常管理:
  set-sub [订阅URL] [本地订阅文件]   导入/更换订阅:预取(本地文件>直连>经代理)→预置缓存
                                    →重启→验证节点数>0,失败自动恢复原状;无参数时进入
                                    粘贴模式(回车读整行,免引号,免疫 URL 里的 & ? 等字符);
                                    自动识别配置里的 provider 名(模板 SUB / 自定义配置均可)
  status [-v|-q|--json]             健康一览:订阅/节点/服务/升级/DNS/出网
                                    -v 追加 TUN/路由/缓存明细  -q 只以退出码表达健康
                                    (0=健康 1=降级 2=故障,供监控)  --json 机器可读
  upgrade                           内核+UI 升级(timer 每天 04:17±25min 自动)
  update-ui                         仅升级 metacubexd 面板
  check [yaml]                      用内核 -t 校验配置(只读,免 root)
  apply-conf <yaml>                 应用手工调整的配置(优先热重载;失败自动恢复)
  rollback [版本]                   回滚内核(默认最近备份)
  uninstall                         移除 unit/timer/sysctl(数据保留)
  units                             输出内嵌 unit 内容(离线审查,不动系统)
  log [行数]                        查看服务与自动升级日志
  man                               显示本工具手册

选项:
  -h, -?, --help                    显示本帮助

详细说明: man panixy(deploy 后可用)或 panixy man
EOF
}

man_page() {  # 内嵌 roff 手册:panixy man 查看;deploy 时装入系统,之后 man panixy
cat <<'EOF'
.TH PANIXY 1 "2026-08-27" "panixy 0.0.2" "Panixy 手册"
.SH 名称
panixy \- 基于 mihomo 的 Linux 透明代理网关部署/管理工具(单文件,自包含)
.SH 概要
.B panixy
.I 命令
.RI [ 参数 ...]
.br
.B panixy \-h | \-\-help | \-\?
.SH 描述
以 mihomo 为内核在 Linux 网关上落地透明代理(TUN):进程守护、接管 systemd\-resolved、
内核与面板每日自动升级(双健康检查,失败自动回滚)。
.PP
订阅导入采用「预取 + 预置缓存 + 重启 + 验证」:CLI 以内核同款 UA 预取订阅
(本地文件 > 直连 > 经本机代理),校验后写入 provider 缓存,重启重建 provider,
并以节点数大于 0 为成功标准——无外网环境也可离线导入
(panixy set-sub \fIURL\fR \fI本地订阅文件\fR)。
.SH 命令
.TP
.BI deploy " [订阅URL] [本地订阅文件]"
全新部署,须在解压的离线包根目录运行:放置内核/geo/面板/广告规则,生成配置
(现有配置 > 包内手工 clash.yaml > 通用模板),安装 CLI 与 man 手册,拉起服务;
任一步失败全量回滚。给定参数时随后执行 set\-sub。
.TP
.B install
仅部署 systemd 服务与 sysctl(文件已就位时;deploy 的内部步骤)。
.TP
.BI set-sub " [订阅URL] [本地订阅文件]"
导入/更换订阅。拉取顺序:本地文件 > 直连 > 经本机代理(换被墙订阅时旧节点可当跳板)。
无参数时进入粘贴模式(读整行,URL 含 & ? 等字符无需加引号)。自动识别配置里第一个
proxy\-provider 的名字(模板为 SUB,自定义配置亦可)。
.TP
.BI status " [\-v|\-q|\-\-json]"
健康一览。\-v 追加 TUN/路由/缓存明细;\-q 静默,仅以退出码表达健康
(0 健康 / 1 降级(服务在但节点为 0 或代理出网不通) / 2 故障(服务或 API 不可用));
\-\-json 输出单行 JSON。
.TP
.B upgrade
内核与面板升级(timer 每天 04:17±25min 自动执行;全成功才更新时间戳)。
.TP
.B update\-ui
仅升级 metacubexd 面板。
.TP
.BI check " [yaml]"
用内核 \-t 校验配置文件(默认当前配置;只读,免 root)。
.TP
.BI apply\-conf " <yaml>"
应用手工调整的配置(优先 API 热重载;失败退重启,再失败恢复原配置)。
.TP
.BI rollback " [版本]"
回滚内核(默认最近备份;内核保留 3 份)。
.TP
.B uninstall
移除 unit/timer/sysctl;数据目录与配置保留。
.TP
.B units
输出内嵌 unit 内容(离线审查,不动系统)。
.TP
.BI log " [行数]"
查看服务与自动升级日志(journalctl)。
.TP
.B man
显示本手册(等价 deploy 后的 man panixy)。
.SH 选项
.TP
.B \-h, \-\-help, \-\?
显示帮助并退出。
.SH 文件
.TP
.I /opt/panixy/
家目录:bin/mihomo 内核、ui/official 面板、proxies/SUB.yaml 订阅缓存、
rule_provider/ 规则缓存、geo 数据、cache.db。
.TP
.I /etc/clash.yaml
配置(唯一事实源:secret/端口改动 CLI 自动跟随)。
.TP
.I /usr/local/bin/panixy , /usr/local/share/man/man1/panixy.1.gz
CLI 与手册(deploy 安装)。
.SH 示例
.nf
sudo ./panixy deploy 'https://订阅链接'        # 离线包内一步部署
sudo ./panixy set\-sub 'URL' sub.yaml          # 无外网时离线导入订阅
panixy status \-\-json                          # 监控取数
panixy status \-q || echo 告警                  # 退出码巡检
.fi
.SH 另见
项目 README(架构/故障排查),mihomo 文档。
EOF
}

cmd_manual() {  # panixy man — 就地阅读内嵌手册(无需安装)
  local t; t=$(mktemp)
  man_page > "$t"
  if command -v man >/dev/null 2>&1 && man -l "$t" 2>/dev/null; then :
  elif command -v groff >/dev/null 2>&1 && groff -man -Tutf8 "$t" 2>/dev/null | col -bx 2>/dev/null; then :
  else sed -e 's/^\.TH.*//' -e 's/^\.\\".*//' "$t"
  fi
  rm -f "$t"
}

deploy_rollback() {  # deploy 失败路径:恢复系统与文件到运行前状态(全部幂等)
  log "部署失败——回滚到运行前状态"
  systemctl disable --now panixy.service panixy-upgrade.timer >/dev/null 2>&1 || true
  rm -f "$UNIT_DIR/panixy.service" "$UNIT_DIR/panixy-upgrade.service" "$UNIT_DIR/panixy-upgrade.timer"
  systemctl daemon-reload >/dev/null 2>&1 || true
  rm -f "$SYSCTL_FILE"
  sysctl -w net.ipv4.ip_forward="${PREV_FWD:-0}" >/dev/null 2>&1 || true
  if [ "${CONF_NEW:-0}" = 1 ]; then rm -f "$CONF"; fi
  if [ "${CLI_NEW:-0}"  = 1 ]; then rm -f "$CLI";  fi
  if [ "${MAN_NEW:-0}"  = 1 ]; then rm -f "$MAN_GZ"; fi
  if [ "${OPT_NEW:-0}"  = 1 ]; then
    rm -rf "$ROOT"
  else
    log "(注意: $ROOT 原本已存在,本次新增文件保留在原地)"
  fi
  log "回滚完成,系统已恢复原状"
}

cmd_deploy() {  # 用法(离线包根目录): panixy deploy [订阅URL] [本地订阅文件]
  # 全部职责:资产就位→配置→CLI+手册→服务→可选订阅导入;事务式,失败全量回滚
  need_root; lock
  local pkg; pkg=$(cd "$(dirname "$0")" && pwd)
  [ -d "$pkg/assets" ] || die "未找到离线资产目录 $pkg/assets —— deploy 需在解压的 Panixy 离线包内运行(已安装的机器用 set-sub/status 等即可)"

  # 注意:子壳不能写成 `( ... ) || die` —— bash 会因 || 上下文忽略子壳内的 set -e,
  # 必须独立语句执行后再检查退出码,事务性才真实生效
  local rc=0
  (
    set -e
    trap 'rc=$?; [ "$rc" -ne 0 ] && deploy_rollback' EXIT

    # 环境预检
    case "$(uname -m)" in
      x86_64)  A=amd64 ;;
      aarch64) A=arm64 ;;
      *) die "不支持的架构: $(uname -m) (包内置 amd64/arm64)" ;;
    esac
    command -v systemctl >/dev/null || die "需要 systemd"
    command -v curl      >/dev/null || die "需要 curl"

    # 运行前状态快照(回滚依据)
    OPT_NEW=0; CONF_NEW=0; CLI_NEW=0; MAN_NEW=0
    [ -d "$ROOT" ] || OPT_NEW=1
    [ -f "$CONF" ] || CONF_NEW=1
    [ -f "$CLI"  ] || CLI_NEW=1
    [ -f "$MAN_GZ" ] || MAN_NEW=1
    PREV_FWD=$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || echo 0)

    mkdir -p "$ROOT/bin" "$ROOT/ui"

    # 1. 内核:已存在则尊重现场不覆盖
    if [ ! -x "$BIN" ]; then
      if [ "$A" = amd64 ] && ! grep -qm1 avx2 /proc/cpuinfo; then
        die "x86_64 CPU 不支持 AVX2,包内 amd64-v3 内核无法运行(会 SIGILL)。方案:手动放置 compatible 版内核到 $BIN 后重跑,或换支持 AVX2 的机器"
      fi
      gz=$(ls "$pkg"/assets/core/mihomo-linux-$A-*.gz 2>/dev/null | sort -V | tail -1 || true)
      [ -n "$gz" ] || die "assets 缺 $A 内核"
      # 注意:不能写成 gzip&&chmod 两连(左元失败会被 set -e 吞掉)
      gzip -dc "$gz" > "$BIN"
      chmod 755 "$BIN"
      [ -s "$BIN" ] || die "解包后内核为空(资产损坏)"
      "$BIN" -v >/dev/null 2>&1 || die "内核无法执行(资产损坏/架构不符)"
      log "[1/5] 内核: $($BIN -v | head -1)"
    else
      log "[1/5] 内核已存在,保留: $($BIN -v | head -1)"
    fi

    # 2. geo 数据 + 分流规则(规则源国内直连不可达,离线预置首启即生效)
    for f in GeoIP.dat GeoSite.dat Country.mmdb; do
      if [ ! -f "$ROOT/$f" ]; then cp "$pkg/assets/geo/$f" "$ROOT/$f"; fi
    done
    mkdir -p "$ROOT/rule_provider"
    if [ -f "$pkg/assets/rule/AWAvenue-Ads.yaml" ]; then
      [ -f "$ROOT/rule_provider/AWAvenue-Ads.yaml" ] || cp "$pkg/assets/rule/AWAvenue-Ads.yaml" "$ROOT/rule_provider/"
    else
      log "  (包内未带广告规则文件,首启将由内核联网拉取)"
    fi
    log "[2/5] geo 与规则数据就位"

    # 3. Web 管理面板(metacubexd,之后由 panixy upgrade 自动更新)
    if [ ! -d "$UI_DIR" ]; then
      cp -r "$pkg/assets/ui/official" "$UI_DIR"
      echo "unknown" > "$UI_STAMP"
    fi
    log "[3/5] Web UI 就位 (http://<本机IP>:9999/ui/)"

    # 4. 配置:现有 > 包内手工 > 通用模板
    if [ -f "$CONF" ]; then
      log "[4/5] 检测到现有配置,保留不动: $CONF"
    elif [ -f "$pkg/clash.yaml" ]; then
      cp "$pkg/clash.yaml" "$CONF"
      log "[4/5] 采用包内手工配置: $pkg/clash.yaml -> $CONF"
    else
      cp "$pkg/assets/clash-template.yaml" "$CONF"
      SECRET_NEW=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
      sed -i "s/^secret: deadship/secret: $SECRET_NEW/" "$CONF"
      log "[4/5] 写入通用模板 $CONF —— 记得导入订阅: panixy set-sub '<订阅URL>'(无外网可离线导入)"
      log "      面板 API 密钥(随机生成): $SECRET_NEW  (日后查看: grep '^secret' $CONF)"
    fi

    # 5. CLI + 手册 + 服务(panixy install 内含预检/健康验证/失败自回滚)
    if [ "$(readlink -f "$0")" != "$(readlink -f "$CLI")" ]; then
      mkdir -p "$(dirname "$CLI")"
      install -m755 "$0" "$CLI"
    fi
    mkdir -p "$(dirname "$MAN_GZ")"
    man_page | gzip -c > "$MAN_GZ"
    log "[5/5] CLI/手册就位,启动服务:"
    cmd_install
    trap - EXIT
  )
  rc=$?
  [ "$rc" -eq 0 ] || die "部署失败,已回滚到运行前状态(详见上方日志)"

  if [ "$#" -gt 0 ]; then
    cmd_set_sub "$@"
  else
    local u=""
    # 只在 stdin 是终端时询问(自动化/管道场景不打扰、不阻塞)
    if [ -t 0 ]; then
      read -r -p "现在导入订阅?粘贴订阅链接(直接回车跳过,稍后 sudo panixy set-sub): " u || u=""
    fi
    if [ -n "$u" ]; then
      cmd_set_sub "$u"
    else
      log "deploy 完成。提示: sudo panixy set-sub 设置订阅(回车进入粘贴模式);panixy status 查看健康"
    fi
  fi
}

case "${1:-}" in
  deploy)     shift; cmd_deploy "$@" ;;
  install)    cmd_install ;;
  upgrade)    cmd_upgrade ;;
  update-ui)  need_root; lock; update_ui ;;
  set-sub)    shift; cmd_set_sub "$@" ;;
  check)      shift; cmd_check "$@" ;;
  apply-conf) shift; cmd_apply_conf "$@" ;;
  status)     shift; cmd_status "$@" ;;
  rollback)   shift; cmd_rollback "$@" ;;
  uninstall)  cmd_uninstall ;;
  units)      cmd_units ;;
  log)        shift; cmd_log "$@" ;;
  man)        cmd_manual ;;
  -h|-\?|--help|help) usage; exit 0 ;;
  *) usage; exit 1 ;;
esac
