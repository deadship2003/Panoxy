#!/usr/bin/env bash
# panixy 冒烟测试:沙箱内验证 set-sub 订阅导入链路,不动系统、不需要 root、不联外网
# 覆盖:可达订阅导入 / 不可达订阅诚实失败 / 本地文件离线导入 / 旧配置自动补 path / status 节点行
# 依赖:bash curl python3 + mihomo 内核(默认取 /opt/panixy/bin/mihomo,可用参数或 MIHOMO_BIN 覆盖)
# 用法:tests/smoke.sh [mihomo内核路径]   全部通过 exit 0
set -u
cd "$(dirname "$0")/.."

MIHOMO="${1:-${MIHOMO_BIN:-/opt/panixy/bin/mihomo}}"
GEO_SRC="${GEO_SRC:-/opt/panixy}"
[ -x "$MIHOMO" ] || { echo "缺 mihomo 内核: $MIHOMO (用法: tests/smoke.sh <内核路径>)"; exit 1; }
command -v python3 >/dev/null || { echo "缺 python3"; exit 1; }
command -v curl     >/dev/null || { echo "缺 curl"; exit 1; }

T=$(mktemp -d /tmp/panixy-smoke.XXXXXX)
# 端口基址随机(20000-39999):避免与上次异常退出残留的监听/本机其他实例撞车
# 注意:ss 第 4 列才是本端地址,不能整行 grep(地址后还有进程信息,$ 锚永不命中)
BASE=$(( (RANDOM % 20000) + 20000 ))
ports_busy() { ss -tln 2>/dev/null | awk '{print $4}' | grep -qE ":(${BASE}|$((BASE+1))|$((BASE+2))|$((BASE+3)))\$"; }
while ports_busy; do BASE=$(( (RANDOM % 20000) + 20000 )); done
CTL=$BASE; MIX=$((BASE+1)); DNP=$((BASE+2)); SRV=$((BASE+3))
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }
cleanup() {
  [ -n "${KEEP:-}" ] && { echo "(KEEP=1,现场保留: $T)"; return; }
  [ -f "$T/pid.svr" ] && kill "$(cat "$T/pid.svr")" 2>/dev/null
  local f
  for f in "$T"/pid.*; do [ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null; done
  rm -rf "$T"
}
trap cleanup EXIT

# ---- 沙箱:假 root / 假 systemctl / 去掉 need_root 的 CLI 副本 ----
mkdir -p "$T/root/bin" "$T/root/ui/official" "$T/units" "$T/bin"
cp "$MIHOMO" "$T/root/bin/mihomo"
touch "$T/root/ui/official/index.html"
sed 's/^  need_root; lock$/  lock/' panixy > "$T/panixy"; chmod +x "$T/panixy"
# 假 systemctl:restart=按 PANIXY_CONF/PANIXY_ROOT 起沙箱内核(9>&- 防止继承 flock),is-active=按 pid 判断
cat > "$T/bin/systemctl" <<SH
#!/bin/sh
# pid 文件按 PANIXY_ROOT 区分:同一轮测试里 root/root2/root3 各有实例,不能互相覆盖
PIDF="$T/pid.\$(basename "\${PANIXY_ROOT:-none}")"
start_mh() {
  nohup "\$PANIXY_ROOT/bin/mihomo" -f "\$PANIXY_CONF" -d "\$PANIXY_ROOT" >> "\$PANIXY_ROOT/run.log" 2>&1 9>&- &
  echo \$! > "\$PIDF"
}
case "\$1" in
  restart) [ -f "\$PIDF" ] && kill \$(cat "\$PIDF") 2>/dev/null; sleep 1; start_mh ;;
  enable)  [ "\$2" = "--now" ] && [ "\$3" = panixy.service ] && start_mh ;;
  is-active) if [ -f "\$PIDF" ] && kill -0 \$(cat "\$PIDF") 2>/dev/null; then echo active; else echo inactive; exit 3; fi ;;
esac
exit 0
SH
chmod +x "$T/bin/systemctl"
# 假 ip/sysctl:非 root 环境 stub 掉 TUN 检查与内核参数写入(仅沙箱 PATH 生效)
printf '#!/bin/sh\nexit 0\n' > "$T/bin/ip";      chmod +x "$T/bin/ip"
printf '#!/bin/sh\nexit 0\n' > "$T/bin/sysctl";  chmod +x "$T/bin/sysctl"

# 假订阅服务器:任意路径返回 4 节点 Clash YAML(模拟机场);启动探活,撞端口自动换
srv_start() {
  python3 - "$SRV" <<'PY' &
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
class H(BaseHTTPRequestHandler):
    def do_GET(self):
        nodes = "".join(f"  - name: 'smoke-{i}'\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\n"
                        for i in range(4))
        body = f"proxies:\n{nodes}".encode()
        self.send_response(200); self.send_header('Content-Length', str(len(body)))
        self.end_headers(); self.wfile.write(body)
    def log_message(self, *a): pass
HTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
PY
  echo $! > "$T/pid.svr"
}
srv_start; sleep 1
for i in 1 2 3 4 5; do
  curl -fsS -m 2 "http://127.0.0.1:$SRV/probe" >/dev/null 2>&1 && break
  kill "$(cat "$T/pid.svr")" 2>/dev/null
  SRV=$(( (RANDOM % 20000) + 20000 )); srv_start; sleep 1
done
curl -fsS -m 2 "http://127.0.0.1:$SRV/probe" >/dev/null 2>&1 || { echo "假订阅服务器无法启动"; exit 1; }

# 测试配置:保留改动点(SUB path / #dns 接线),去掉 tun(无 root 不能建设备)
CONF=$T/clash.yaml
cat > "$CONF" <<EOF
mixed-port: $MIX
mode: rule
log-level: warning
ipv6: false
external-controller: 127.0.0.1:$CTL
secret: deadship
dns:
  enable: true
  listen: 127.0.0.1:$DNP
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  default-nameserver: [223.5.5.5]
  nameserver:
    - https://1.1.1.1/dns-query#dns
  proxy-server-nameserver:
    - https://doh.pub/dns-query
proxy-providers:
  SUB:
    type: http
    url: "SUB_URL_PLACEHOLDER"
    path: ./proxies/SUB.yaml
    interval: 3600
    health-check: {enable: false}
proxy-groups:
  - { name: dns, type: select, use: [SUB], proxies: [🔃 自动选择] }
  - { name: 🚀 节点选择, type: select, use: [SUB], proxies: [🔃 自动选择] }
  - { name: 🔃 自动选择, type: url-test, use: [SUB], tolerance: 2 }
rules:
  - MATCH,🚀 节点选择
EOF

api_nodes() {  # 内核当前加载的 SUB 节点数
  curl -s -m 4 -H "Authorization: Bearer deadship" "http://127.0.0.1:$CTL/providers/proxies/SUB" \
    | grep -o '"provider-name"' | wc -l
}
run_cli() {  # 以沙箱环境运行被测 CLI
  PATH="$T/bin:$PATH" PANIXY_ROOT="$T/root" PANIXY_CONF="$CONF" PANIXY_UNIT_DIR="$T/units" \
  PANIXY_LOCK="$T/lck" PANIXY_API_PORT=$CTL PANIXY_PROXY_PORT=$MIX PANIXY_SECRET=deadship \
  PANIXY_SYSCTL="$T/99.conf" "$T/panixy" "$@"
}

echo "== 0. 模板过内核 -t(需 geo 数据;缺则跳过)=="
# -t 只做解析(不绑端口、不建 TUN、不拉订阅),geo 文件放进沙箱家目录即可
if [ -f "$GEO_SRC/GeoSite.dat" ]; then
  cp "$GEO_SRC"/GeoIP.dat "$GEO_SRC"/GeoSite.dat "$GEO_SRC"/Country.mmdb "$T/root/" 2>/dev/null
  if "$T/root/bin/mihomo" -t -f clash-template.yaml -d "$T/root" >/dev/null 2>&1; then ok "clash-template.yaml -t 通过"
  else bad "clash-template.yaml -t 未通过"; fi
else
  echo "  - 跳过(未找到 $GEO_SRC/GeoSite.dat)"
fi

echo "== 1. 可达订阅:set-sub 应成功并验证节点加载 =="
if run_cli set-sub "http://127.0.0.1:$SRV/sub?token=ok" > "$T/out1" 2>&1; then ok "exit 0"
else bad "exit 非 0: $(tail -3 "$T/out1")"; fi
grep -q '加载成功:4 个节点' "$T/out1" && ok "日志报告 4 节点" || bad "未见节点数报告"
grep -q "token=ok" "$CONF" && ok "URL 已写入配置" || bad "URL 未写入"
[ "$(api_nodes)" -ge 4 ] && ok "内核实际加载 $(api_nodes) 节点" || bad "内核节点数=$(api_nodes)"
[ -s "$T/root/proxies/SUB.yaml" ] && ok "缓存已预置 proxies/SUB.yaml" || bad "缓存未预置"

echo "== 2. 不可达订阅:应诚实失败且配置零改动 =="
before=$(grep -m1 '^    url:' "$CONF")
if run_cli set-sub 'http://192.0.2.1:8080/sub?token=dead' > "$T/out2" 2>&1; then
  bad "不可达订阅竟报成功"
else
  ok "exit 非 0"; grep -q '订阅拉取失败' "$T/out2" && ok "报错含离线导入指引" || bad "报错信息不符"
fi
[ "$(grep -m1 '^    url:' "$CONF")" = "$before" ] && ok "配置未被改动" || bad "配置被改动"

echo "== 3. 被墙订阅 + 本地文件:离线导入应成功 =="
printf 'proxies:\n  - name: offline-x\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\n' > "$T/seed.yaml"
if run_cli set-sub 'https://blocked.example.com/sub?token=w' "$T/seed.yaml" > "$T/out3" 2>&1; then ok "exit 0"
else bad "exit 非 0: $(tail -3 "$T/out3")"; fi
[ "$(api_nodes)" -eq 1 ] && ok "内核加载了离线文件的 1 节点" || bad "内核节点数=$(api_nodes)"

echo "== 4. 旧版配置(无 path):set-sub 应自动补写 =="
grep -v '^    path:' "$CONF" > "$T/nopath" && mv "$T/nopath" "$CONF"
run_cli set-sub "http://127.0.0.1:$SRV/sub?token=v2" > "$T/out4" 2>&1
grep -q '^    path: ./proxies/SUB.yaml' "$CONF" && ok "path 已自动补写" || bad "path 未补写"

echo "== 4b. 自定义 provider 名(airport):set-sub/status 应自动识别 =="
sed -e 's/^  SUB:/  airport:/' -e 's/use: \[SUB\]/use: [airport]/' "$CONF" > "$T/air" && mv "$T/air" "$CONF"
rm -rf "$T/root/proxies"
if PATH="$T/bin:$PATH" PANIXY_ROOT="$T/root" PANIXY_CONF="$CONF" PANIXY_UNIT_DIR="$T/units" \
     PANIXY_LOCK="$T/lck" PANIXY_API_PORT=$CTL PANIXY_PROXY_PORT=$MIX PANIXY_SECRET=deadship \
     PANIXY_SYSCTL="$T/99.conf" bash -x "$T/panixy" set-sub "http://127.0.0.1:$SRV/sub?token=air" > "$T/out4b" 2>&1; then ok "exit 0"
else bad "自定义 provider 名失败: $(grep 'panixy\]' "$T/out4b" | tail -2)"; fi
grep -q 'provider airport' "$T/out4b" && ok "日志识别 provider 名 airport" || bad "未识别 provider 名"
grep -A2 '^  airport:' "$CONF" | grep -q "token=air" && ok "URL 写入 airport 块" || bad "URL 未写入 airport 块"
run_cli status > "$T/st4b" 2>&1
grep -q '^节点:     4 个' "$T/st4b" && ok "status 按名统计 airport 节点" || bad "status 节点行异常: $(grep '^节点' "$T/st4b")"

echo "== 4c. 无参数粘贴模式(管道喂 URL,免引号场景)== "
printf 'http://127.0.0.1:%s/sub?token=paste&sid=x&flag=1\n' "$SRV" | run_cli set-sub > "$T/out4c" 2>&1
grep -q 'token=paste&sid=x&flag=1' "$CONF" && ok "整行 URL(含多个 & )完整写入" || bad "粘贴模式 URL 不完整"
sed -i -e 's/^  airport:/  SUB:/' -e 's/use: \[airport\]/use: [SUB]/' "$CONF"   # 还原名
run_cli set-sub "http://127.0.0.1:$SRV/sub?token=back" >/dev/null 2>&1          # 重启内核对齐改名后的配置

echo "== 5. status 展示节点行 =="
run_cli status > "$T/out5" 2>&1
grep -q '^节点:     4 个' "$T/out5" && ok "status 显示 4 节点" || bad "status 节点行异常: $(grep '^节点' "$T/out5")"

echo "== 6. 帮助与 status 参数 =="
for f in -h -\? --help help; do
  if run_cli "$f" > "$T/help.out" 2>&1 && grep -q 'panixy v.* —' "$T/help.out"; then ok "$f 输出帮助"
  else bad "$f 未输出帮助(exit=$?)"; fi
done
run_cli man > "$T/man.out" 2>&1 && grep -q 'panixy' "$T/man.out" && ok "man 输出手册" || bad "man 异常"
run_cli status --json > "$T/json.out" 2>&1
grep -q '"nodes":4' "$T/json.out" && grep -q '"service":"active"' "$T/json.out" \
  && ok "status --json 机器可读" || bad "status --json 异常: $(cat "$T/json.out")"
run_cli status -q 2>/dev/null; rc=$?
[ "$rc" -eq 1 ] && ok "status -q 退出码=1(节点在但代理出网不通=降级,沙箱预期)" || bad "status -q 退出码=$rc(期望1)"
run_cli status -v > "$T/v.out" 2>&1 && grep -q -- '-- 详细' "$T/v.out" && ok "status -v 追加明细" || bad "status -v 异常"

echo "== 7. deploy:离线包一步部署(成功路径)=="
# 组装迷你离线包:内核 gz + geo + 面板 + 规则 + 包内手工 clash.yaml(无 tun,沙箱非 root)
PKG=$T/pkg
mkdir -p "$PKG/assets/core" "$PKG/assets/geo" "$PKG/assets/ui/official" "$PKG/assets/rule"
cp "$T/panixy" "$PKG/panixy"; chmod +x "$PKG/panixy"
gzip -c "$T/root/bin/mihomo" > "$PKG/assets/core/mihomo-linux-amd64-v1.99.99-smoke.gz"
cp "$GEO_SRC"/GeoIP.dat "$GEO_SRC"/GeoSite.dat "$GEO_SRC"/Country.mmdb "$PKG/assets/geo/" 2>/dev/null
touch "$PKG/assets/ui/official/index.html"
printf 'payload:\n  - "+.ad.example"\n' > "$PKG/assets/rule/AWAvenue-Ads.yaml"
sed "s|SUB_URL_PLACEHOLDER|http://127.0.0.1:$SRV/sub?token=pkg|" "$CONF" > "$PKG/clash.yaml"
R2=$T/root2 C2=$T/deploy.yaml
rm -rf "$R2" "$C2"
if PATH="$T/bin:$PATH" PANIXY_ROOT="$R2" PANIXY_CONF="$C2" PANIXY_UNIT_DIR="$T/units" \
     PANIXY_LOCK="$T/lck2" PANIXY_CLI="$T/cli/panixy" PANIXY_MAN="$T/man/panixy.1.gz" \
     PANIXY_API_PORT=$CTL PANIXY_PROXY_PORT=$MIX PANIXY_SECRET=deadship PANIXY_SYSCTL="$T/99b.conf" \
     "$PKG/panixy" deploy > "$T/dep.out" 2>&1; then
  ok "deploy exit 0"
else
  bad "deploy 失败: $(tail -5 "$T/dep.out")"
fi
[ -x "$R2/bin/mihomo" ] && ok "内核由 gz 解包就位" || bad "内核未就位"
[ -f "$R2/rule_provider/AWAvenue-Ads.yaml" ] && ok "广告规则离线预置" || bad "规则未预置"
[ -f "$R2/GeoSite.dat" ] && ok "geo 就位" || bad "geo 未就位"
grep -q 'secret: deadship' "$C2" && ok "包内手工配置被采用" || bad "配置来源不对"
[ -x "$T/cli/panixy" ] && ok "CLI 安装到指定路径" || bad "CLI 未安装"
[ -s "$T/man/panixy.1.gz" ] && ok "man 手册已安装" || bad "手册未安装"
n2=$(curl -s -m 4 -H "Authorization: Bearer deadship" "http://127.0.0.1:$CTL/providers/proxies/SUB" | grep -o '"provider-name"' | wc -l)
[ "$n2" -ge 4 ] && ok "deploy 后内核加载 $n2 节点(模板订阅缓存路径全程可用)" || bad "deploy 后节点数=$n2"

echo "== 8. deploy 失败全量回滚(内核资产损坏→中止并清理新建文件)=="
R3=$T/root3 C3=$T/deploy3.yaml
rm -rf "$R3" "$C3" "$T/man3" "$T/pkg3" "$T/cli3"
PKG3=$T/pkg3
mkdir -p "$PKG3/assets/core" "$PKG3/assets/geo" "$PKG3/assets/ui/official" "$PKG3/assets/rule"
cp "$T/panixy" "$PKG3/panixy"; chmod +x "$PKG3/panixy"
printf 'corrupt-gzip-for-rollback-test\n' > "$PKG3/assets/core/mihomo-linux-amd64-v0.0.0-corrupt.gz"
cp "$GEO_SRC"/GeoIP.dat "$PKG3/assets/geo/" 2>/dev/null
touch "$PKG3/assets/ui/official/index.html"
printf 'payload:\n  - "+.ad.example"\n' > "$PKG3/assets/rule/AWAvenue-Ads.yaml"
cp /home/xx/Panixy/clash-template.yaml "$PKG3/assets/clash-template.yaml"
if PATH="$T/bin:$PATH" PANIXY_ROOT="$R3" PANIXY_CONF="$C3" PANIXY_UNIT_DIR="$T/units" \
     PANIXY_LOCK="$T/lck3" PANIXY_CLI="$T/cli3/panixy" PANIXY_MAN="$T/man3/panixy.1.gz" \
     PANIXY_API_PORT=38195 PANIXY_SECRET=deadship PANIXY_SYSCTL="$T/99c.conf" \
     timeout 60 "$PKG3/panixy" deploy > "$T/dep3.out" 2>&1; then
  bad "损坏内核竟部署成功"
else
  ok "deploy 失败 exit 非 0"
fi
[ ! -d "$R3" ] && ok "回滚:新建 ROOT 已删除" || bad "回滚不全: ROOT 残留"
[ ! -f "$C3" ] && ok "回滚:配置未残留" || bad "回滚不全: 配置残留"
[ ! -e "$T/man3/panixy.1.gz" ] && ok "回滚:手册未残留" || bad "回滚不全: 手册残留"
grep -q '回滚' "$T/dep3.out" && ok "日志含回滚说明" || bad "日志无回滚说明"

echo
echo "== 结果:$PASS 通过,$FAIL 失败 =="
[ "$FAIL" -eq 0 ]
