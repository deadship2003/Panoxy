# =====================================================================
# panixy 基础配置 v0.2.0(Go 版,由 panixy 渲染)
# ★ proxy-providers 段由 set-sub/sub-rm 增量管理,请勿手改该段(其余随意)
# ★ 端口/密钥改动后 panixy 自动跟随 —— 配置文件是唯一事实源
#
# v0.2.0 要点(相对 bash 版 v0.1.4):
#   - DNS 劫持由 nftables/iptables 完成(redirect→1053),已删除 tun.dns-hijack
#   - routing-mark 6666:mihomo 自身出站标记,防火墙据此放行防 DNS 回环(勿改)
#   - tun.stack 默认 system(家用低负载够用);重度 BT/PT、长时间 UDP 流媒体、
#     节点频繁掉线、老内核(5.4/5.15)建议改 gvisor 无人值守兜底
#   - 境外域名真实解析经 dns 组(默认 🔃 自动选择=最快节点)走 DoH
#   - NTP 国内源;prefer-h3 关闭(UDP 被扰动时 DNS 不陪葬)
# =====================================================================

# ============ 锚点模板(&pr 服务组 / &prd 国内组 / &p 订阅 / &use 通用组)============
# set-sub 会把订阅名追加进下列 use 列表:一处修改,全部消费组生效
pr: &pr
  type: select
  proxies: [🔃 自动选择, 香港, 台湾, 日本, 新加坡, 韩国, 美国, 其他地区, 全部节点, DIRECT]
  use: [SUB]
prd: &prd
  type: select
  proxies: [DIRECT, 🔃 自动选择, 香港, 台湾, 日本, 新加坡, 韩国, 美国, 其他地区, 全部节点]
  use: [SUB]
# 订阅 provider 公共模板:set-sub --name 生成的条目以 <<: *p 复用
p: &p
  type: http
  interval: 86400
  health-check:
    enable: true
    url: https://www.gstatic.com/generate_204/
    interval: 300
    timeout: 1000
    tolerance: 100
use: &use
  type: select
  use: [SUB]

# ============ 订阅源(set-sub --name 管理)============
# 默认占位订阅:status 检测到 SUB_URL_PLACEHOLDER 即提示"尚未设置";
# set-sub 默认改写 SUB;--name 其他名称则新增并列条目
proxy-providers:
  SUB:
    <<: *p
    url: "SUB_URL_PLACEHOLDER"
    path: ./proxies/SUB.yaml

# ============ 基础设置 ============
mixed-port: {{.MixedPort}}
port: 6666
socks-port: 6699
allow-lan: true
mode: rule
log-level: warning
ipv6: true
unified-delay: true
tcp-concurrent: true
# 网关/路由器保持 off(转发流量无进程信息)
find-process-mode: off
keep-alive-interval: 30
# mihomo 自身出站打标 6666,与防火墙排除规则联动防 DNS 回环(勿改)
routing-mark: {{.RoutingMark}}

# ============ 外部控制 / 面板 ============
external-controller: 0.0.0.0:{{.ApiPort}}
external-ui: ui/official
secret: {{.Secret}}

{{- if .TProxy}}
# ============ TPROXY 模式(mark/策略路由/劫持链由 panixy 防火墙模块管理)============
tproxy-port: {{.TproxyPort}}
{{- else}}
# ============ TUN 透明代理(DNS 劫持已移交防火墙,故无 dns-hijack)============
tun:
  enable: true
  stack: system             # 家用默认;易触发 TUN 静默卡死的场景建议改 gvisor
  auto-route: true
  auto-detect-interface: true
  strict-route: true
  mtu: 1500
  # 排除回环/内网,防代理循环
  route-exclude-address:
    - 127.0.0.0/8
    - ::1/128
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
{{- end}}
iptables:
  enable: false

# ============ Geo 数据 ============
geodata-mode: true
geo-auto-update: true
geo-update-interval: 24
geox-url:
  geoip: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat"
  geosite: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
  mmdb: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country.mmdb"

# ============ 缓存 ============
profile:
  store-selected: true
  store-fake-ip: true

# ============ NTP ============
ntp:
  enable: true
  write-to-system: false
  server: ntp.aliyun.com   # 国内网关 time.apple.com(UDP 123)常被扰动
  port: 123
  interval: 30

# ============ 域名嗅探 ============
sniffer:
  enable: true
  sniff:
    TLS:
      ports: [443, 8443]
    HTTP:
      ports: [80, 8080-8880]
      override-destination: true

# ============ DNS ============
dns:
  enable: true
  ipv6: true
  # 防火墙 redirect 的落点:必须 0.0.0.0(PREROUTING 的 LAN 客户端也要到达),勿改回 127.0.0.1
  listen: 0.0.0.0:{{.DnsPort}}
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  prefer-h3: false          # DoH 走 H2/TCP
  respect-rules: true
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - "*.direct"
    - time.windows.com
    - ntp.*
  default-nameserver:
    - 223.5.5.5
    - 119.29.29.29
  # 境外域名真实解析:经 dns 组(默认 🔃 自动选择=最快节点)走 DoH;
  # fake-ip 模式下客户端拿假 IP,这里仅在内核需要真实 IP 时触发
  nameserver:
    - https://1.1.1.1/dns-query#dns
  # 订阅域名/节点服务器域名:国内直连可解析(引导期生命线,勿走代理)
  proxy-server-nameserver:
    - https://doh.pub/dns-query
  nameserver-policy:
    "geosite:cn,private":
      - https://doh.pub/dns-query
      - 223.5.5.5
      - 119.29.29.29

# ============ 策略分组 ============
proxy-groups:
  # dns 组:上方 nameserver 里 DoH 的出站口(#dns 后缀),默认走 🔃 自动选择(最快节点)
  # 刻意不含 DIRECT(避免被墙域名走直连无法解析)
  - { name: dns, <<: *use, proxies: [🔃 自动选择, 香港, 台湾, 日本, 新加坡, 韩国, 美国, 全部节点] }
  - { name: 🚀 节点选择, <<: *pr }
  - { name: 学术, <<: *pr }
  - { name: 境外AI, <<: *pr }
  - { name: 广告拦截, type: select, proxies: [REJECT, DIRECT, 🚀 节点选择] }
  - { name: TikTok, <<: *pr }
  - { name: 🎥 油管视频, <<: *pr }
  - { name: 🤖 ChatGPT, <<: *pr }
  - { name: 🍎 苹果服务, <<: *pr }
  - { name: Ⓜ️ 微软云盘, <<: *pr }
  - { name: Ⓜ️ 微软, <<: *pr }
  - { name: 🎮 游戏平台, <<: *pr }
  - { name: 📢 谷歌FCM, <<: *pr }
  - { name: 📲 电报服务, <<: *pr }
  - { name: Twitter, <<: *pr }
  - { name: Copilot, <<: *pr }
  - { name: Synology, <<: *pr }
  - { name: qBitorrent, <<: *pr }
  - { name: 🎥 网飞视频, <<: *pr }
  - { name: 国内, <<: *prd }
  - { name: Spotify, <<: *pr }
  - { name: 🔍 GitHub, <<: *pr }
  # 兜底组:所有规则没匹配到的流量
  - { name: 其他, <<: *pr }

  # ===== 地区分组(通配常见机场命名,不中可改各组的 filter)=====
  - { name: 香港, <<: *use, type: url-test, filter: '(?i)港|HK|Hong ?Kong' }
  - { name: 台湾, <<: *use, type: url-test, filter: '(?i)台|新北|彰化|TW|Taiwan' }
  - { name: 日本, <<: *use, type: url-test, filter: '(?i)日本|东京|東京|大阪|埼玉|JP|Japan|Tokyo|Osaka' }
  - { name: 新加坡, <<: *use, type: url-test, filter: '(?i)新加坡|狮城|SG|Singapore' }
  - { name: 韩国, <<: *use, type: url-test, filter: '(?i)韩|首尔|KR|Korea|Seoul' }
  - { name: 美国, <<: *use, type: url-test, filter: '(?i)美国|美西|美东|States|America|洛杉矶|圣何塞|西雅图' }
  - { name: 其他地区, <<: *use, type: url-test, filter: '(?i)马来|泰国|越南|菲律宾|印尼|印度|土耳其|德国|英国|法国|荷兰|俄罗斯|巴西|阿根廷|Malaysia|Thailand|Vietnam|Philippines|Indonesia|Turkey|Germany|Russia|Brazil|Argentina' }
  - { name: 全部节点, <<: *use }
  - { name: 🔃 自动选择, <<: *use, tolerance: 2, type: url-test }

# ============ 规则订阅 ============
rule-providers:
  # 秋风广告拦截规则 https://awavenue.top
  # 源站在 raw.githubusercontent.com(国内直连不可达):离线包已预置同名缓存文件,
  # 首启离线可用;之后内核刷新订阅/规则时流量经自身规则走节点,自动恢复更新
  AWAvenue-Ads:
    type: http
    behavior: domain
    format: yaml
    path: ./rule_provider/AWAvenue-Ads.yaml
    url: "https://raw.githubusercontent.com/TG-Twilight/AWAvenue-Ads-Rule/refs/heads/main/Filters/AWAvenue-Ads-Rule-Clash-Classical.yaml"
    interval: 86400

# ============ 分流规则 ============
rules:
  # ===== 基础服务直连(不走代理,保证 SSH/VPN/mDNS 等正常)=====
  - DST-PORT,22,DIRECT                              # SSH/SFTP
  - DST-PORT,23,DIRECT                              # Telnet
  - DST-PORT,41641,DIRECT                           # Tailscale 直连 UDP
  - DST-PORT,3478,DIRECT                            # STUN/TURN(NAT 穿透)
  - DST-PORT,51820,DIRECT                           # WireGuard
  - DST-PORT,1194,DIRECT                            # OpenVPN
  - DST-PORT,5353,DIRECT                            # mDNS(局域网发现)
  - DST-PORT,123,DIRECT                             # NTP(时间同步)
  - DST-PORT,161,DIRECT                             # SNMP(网管)
  - IP-CIDR,100.100.100.100/32,DIRECT,no-resolve    # Tailscale MagicDNS
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve         # Tailscale 子网(CGNAT)

  # ===== 应用分流 =====
  - GEOSITE,TikTok,TikTok
  - DOMAIN-SUFFIX,chatgpt.com,🤖 ChatGPT
  - DOMAIN-SUFFIX,oaistatic.com,🤖 ChatGPT
  - DOMAIN-SUFFIX,oaiusercontent.com,🤖 ChatGPT
  - DOMAIN-SUFFIX,openai.com,🤖 ChatGPT
  - DOMAIN-SUFFIX,openai.com.cdn.cloudflare.net,🤖 ChatGPT
  - DOMAIN-SUFFIX,openaiapi-site.azureedge.net,🤖 ChatGPT
  - DOMAIN-SUFFIX,openaicom-api-bdcpf8c6d2e9atf6.z01.azurefd.net,🤖 ChatGPT
  - DOMAIN-SUFFIX,openaicomproductionae4b.blob.core.windows.net,🤖 ChatGPT
  - DOMAIN-SUFFIX,production-openaicom-storage.azureedge.net,🤖 ChatGPT
  - RULE-SET,AWAvenue-Ads,广告拦截
  - GEOSITE,category-scholar-!cn,学术
  - GEOSITE,category-ai-!cn,境外AI
  - GEOSITE,steam@cn,DIRECT
  - GEOSITE,steam,🎮 游戏平台
  - GEOSITE,apple,🍎 苹果服务
  - GEOSITE,onedrive,Ⓜ️ 微软云盘
  - GEOSITE,apple-cn,🍎 苹果服务
  - GEOSITE,github,🔍 GitHub
  - GEOSITE,twitter,Twitter
  - DOMAIN-KEYWORD,synology,Synology
  - DOMAIN-KEYWORD,copilot,Copilot
  - GEOSITE,bing,Ⓜ️ 微软
  - GEOSITE,microsoft,Ⓜ️ 微软
  - GEOSITE,msn,Ⓜ️ 微软
  - GEOSITE,youtube,🎥 油管视频
  - GEOSITE,google,📢 谷歌FCM
  - GEOSITE,google-cn,📢 谷歌FCM
  - GEOSITE,telegram,📲 电报服务
  - GEOSITE,netflix,🎥 网飞视频
  - GEOSITE,spotify,Spotify
  - GEOSITE,geolocation-!cn,其他
  - AND,(AND,(DST-PORT,443),(NETWORK,UDP)),(NOT,((GEOIP,CN))),REJECT # quic
  - GEOIP,google,📢 谷歌FCM
  - GEOIP,netflix,🎥 网飞视频
  - GEOIP,telegram,📲 电报服务
  - GEOIP,twitter,Twitter
  - GEOSITE,CN,国内
  - GEOIP,CN,国内
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
  - MATCH,其他
