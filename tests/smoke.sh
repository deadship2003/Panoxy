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
CTL=38190; MIX=38191; DNP=38192; SRV=38193   # 本机高位端口,避免与真实实例冲突
PASS=0; FAIL=0
ok()  { echo "  ✓ $1"; PASS=$((PASS+1)); }
bad() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }
cleanup() {
  [ -f "$T/pid.svr" ] && kill "$(cat "$T/pid.svr")" 2>/dev/null
  [ -f "$T/pid.mh"  ] && kill "$(cat "$T/pid.mh")"  2>/dev/null
  rm -rf "$T"
}
trap cleanup EXIT

# ---- 沙箱:假 root / 假 systemctl / 去掉 need_root 的 CLI 副本 ----
mkdir -p "$T/root/bin" "$T/root/ui/official" "$T/units" "$T/bin"
cp "$MIHOMO" "$T/root/bin/mihomo"
touch "$T/root/ui/official/index.html"
sed 's/^  need_root; lock$/  lock/' panixy > "$T/panixy"; chmod +x "$T/panixy"
# 假 systemctl:restart=按 PANIXY_CONF 起沙箱内核(9>&- 防止继承 flock),is-active=按 pid 判断
cat > "$T/bin/systemctl" <<SH
#!/bin/sh
case "\$1" in
  restart) [ -f "$T/pid.mh" ] && kill \$(cat "$T/pid.mh") 2>/dev/null; sleep 1
    nohup "$T/root/bin/mihomo" -f "\$PANIXY_CONF" -d "$T/root" >> "$T/root/run.log" 2>&1 9>&- &
    echo \$! > "$T/pid.mh" ;;
  is-active) [ -f "$T/pid.mh" ] && kill -0 \$(cat "$T/pid.mh") 2>/dev/null && echo active || echo inactive ;;
esac
exit 0
SH
chmod +x "$T/bin/systemctl"

# 假订阅服务器:任意路径返回 4 节点 Clash YAML(模拟机场)
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
echo $! > "$T/pid.svr"; sleep 1

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
grep -q '订阅加载成功:4 个节点' "$T/out1" && ok "日志报告 4 节点" || bad "未见节点数报告"
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

echo "== 5. status 展示节点行 =="
run_cli status > "$T/out5" 2>&1
grep -q '^节点:     4 个' "$T/out5" && ok "status 显示 4 节点" || bad "status 节点行异常: $(grep '^节点' "$T/out5")"

echo
echo "== 结果:$PASS 通过,$FAIL 失败 =="
[ "$FAIL" -eq 0 ]
