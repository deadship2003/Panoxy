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
  proxies: [🔃 自动选择, 🎬 流媒体, 🎮 游戏, 🇨🇳 回国, 香港, 台湾, 日本, 新加坡, 韩国, 美国, 英国, 德国, 法国, 荷兰, 加拿大, 澳大利亚, 俄罗斯, 土耳其, 印度, 马来西亚, 泰国, 越南, 菲律宾, 印度尼西亚, 巴西, 阿根廷, 其他地区, 全部节点, DIRECT]
  use: [SUB]
prd: &prd
  type: select
  proxies: [DIRECT, 🔃 自动选择, 香港, 台湾, 日本, 新加坡, 韩国, 美国, 英国, 德国, 法国, 荷兰, 加拿大, 澳大利亚, 俄罗斯, 土耳其, 印度, 马来西亚, 泰国, 越南, 菲律宾, 印度尼西亚, 巴西, 阿根廷, 其他地区, 全部节点]
  use: [SUB]
# 订阅 provider 公共模板:set-sub --name 生成的条目以 <<: *p 复用
p: &p
  type: http
  interval: 86400
  health-check:
    enable: true
    url: https://www.gstatic.com/generate_204/
    interval: 300
    timeout: 5000
    lazy: true
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
# 参数与 internal/asset.TunParams / TunRouteExclude 保持一致(改此块务必同步 Go 常量)
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

  # ===== 地区分组(全分组:覆盖绝大多数机场命名;set-sub 会按实际节点剔除无匹配项)=====
  - { name: 香港, <<: *use, type: url-test, filter: '(?i)香港|港|HK|Hong ?Kong|HKG' }
  - { name: 台湾, <<: *use, type: url-test, filter: '(?i)台湾|台北|新北|桃园|台中|台南|彰化|TW|Taiwan|TPE' }
  - { name: 日本, <<: *use, type: url-test, filter: '(?i)日本|东京|東京|大阪|埼玉|冲绳|JP|Japan|Tokyo|Osaka|Narita' }
  - { name: 新加坡, <<: *use, type: url-test, filter: '(?i)新加坡|狮城|SG|Singapore|SIN' }
  - { name: 韩国, <<: *use, type: url-test, filter: '(?i)韩国|韩|首尔|釜山|KR|Korea|Seoul|Busan' }
  - { name: 美国, <<: *use, type: url-test, filter: '(?i)美国|美西|美东|美中|洛杉矶|圣何塞|西雅图|纽约|波特兰|硅谷|US|United ?States|America|Los ?Angeles|San ?Jose|Seattle|New ?York' }
  - { name: 英国, <<: *use, type: url-test, filter: '(?i)英国|英|伦敦|UK|United ?Kingdom|Britain|London' }
  - { name: 德国, <<: *use, type: url-test, filter: '(?i)德国|德|法兰克福|DE|Germany|Frankfurt' }
  - { name: 法国, <<: *use, type: url-test, filter: '(?i)法国|法|巴黎|FR|France|Paris' }
  - { name: 荷兰, <<: *use, type: url-test, filter: '(?i)荷兰|荷|阿姆斯特丹|NL|Netherlands|Amsterdam' }
  - { name: 加拿大, <<: *use, type: url-test, filter: '(?i)加拿大|加|多伦多|温哥华|CA|Canada|Toronto|Vancouver' }
  - { name: 澳大利亚, <<: *use, type: url-test, filter: '(?i)澳大利亚|澳洲|悉尼|墨尔本|AU|Australia|Sydney|Melbourne' }
  - { name: 俄罗斯, <<: *use, type: url-test, filter: '(?i)俄罗斯|俄|莫斯科|RU|Russia|Moscow' }
  - { name: 土耳其, <<: *use, type: url-test, filter: '(?i)土耳其|土|伊斯坦布尔|TR|Turkey|Istanbul' }
  - { name: 印度, <<: *use, type: url-test, filter: '(?i)印度|印|孟买|IN|India|Mumbai' }
  - { name: 马来西亚, <<: *use, type: url-test, filter: '(?i)马来西亚|马来|吉隆坡|MY|Malaysia|Kuala ?Lumpur' }
  - { name: 泰国, <<: *use, type: url-test, filter: '(?i)泰国|泰|曼谷|TH|Thailand|Bangkok' }
  - { name: 越南, <<: *use, type: url-test, filter: '(?i)越南|越|胡志明|河内|VN|Vietnam|Ho ?Chi ?Minh|Hanoi' }
  - { name: 菲律宾, <<: *use, type: url-test, filter: '(?i)菲律宾|菲|马尼拉|PH|Philippines|Manila' }
  - { name: 印度尼西亚, <<: *use, type: url-test, filter: '(?i)印度尼西亚|印尼|雅加达|ID|Indonesia|Jakarta' }
  - { name: 巴西, <<: *use, type: url-test, filter: '(?i)巴西|巴|圣保罗|BR|Brazil|Sao ?Paulo' }
  - { name: 阿根廷, <<: *use, type: url-test, filter: '(?i)阿根廷|阿|布宜诺斯|AR|Argentina|Buenos ?Aires' }
  - { name: 其他地区, <<: *use, type: url-test, filter: '(?i)阿联酋|迪拜|沙特|南非|墨西哥|智利|波兰|瑞典|瑞士|挪威|丹麦|芬兰|奥地利|比利时|西班牙|意大利|葡萄牙|爱尔兰|捷克|乌克兰|UAE|Dubai|Saudi|South ?Africa|Mexico|Chile|Poland|Sweden|Switzerland|Norway|Denmark|Finland|Austria|Belgium|Spain|Italy|Portugal|Ireland|Czech|Ukraine' }

  # ===== 类型分组(按节点用途派生;set-sub 会按实际节点剔除无匹配项)=====
  - { name: 🎬 流媒体, <<: *use, type: url-test, filter: '(?i)流媒体|解锁|原生IP|原生|奈飞|网飞|Netflix|Disney|迪士尼|媒体|Media|影视|4K|Streaming' }
  - { name: 🎮 游戏, <<: *use, type: url-test, filter: '(?i)游戏|电竞|低延迟|Game|Gaming|IEPL|IPLC|专线|内网' }
  - { name: 🇨🇳 回国, <<: *use, type: url-test, filter: '(?i)回国|大陆|中国|China|CN2|回国线路|国内中转' }

  # 兜底组(无 filter,不参与剪枝)
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
# 原则:不阻断任何协议(QUIC/DoT/DoQ/DoH 均纳入正常分流);透明代理的第一目标是正常访问。
# DNS 53 端口照常劫持(为大多数设备提供域名级分流);加密 DNS(853/443)走代理(为自定义设备保留访问)。
rules:
  # ===== 基础服务直连(不走代理,保证 SSH/VPN/远程桌面/VoIP/IoT 等正常)=====
  - DST-PORT,22,DIRECT                              # SSH/SFTP
  - DST-PORT,23,DIRECT                              # Telnet
  - DST-PORT,3389,DIRECT                            # RDP 远程桌面(延迟敏感)
  - DST-PORT,5900,DIRECT                            # VNC 远程桌面
  - DST-PORT,5060,DIRECT                            # SIP(VoIP 信令)
  - DST-PORT,5061,DIRECT                            # SIPS(SIP over TLS)
  - DST-PORT,1900,DIRECT                            # SSDP/UPnP 设备发现
  - DST-PORT,88,DIRECT                              # Kerberos 域认证
  - DST-PORT,389,DIRECT                             # LDAP
  - DST-PORT,636,DIRECT                             # LDAPS
  - DST-PORT,1812,DIRECT                            # RADIUS 认证
  - DST-PORT,1813,DIRECT                            # RADIUS 计费
  - DST-PORT,41641,DIRECT                           # Tailscale 直连 UDP
  - DST-PORT,3478,DIRECT                            # STUN/TURN(NAT 穿透)
  - DST-PORT,51820,DIRECT                           # WireGuard
  - DST-PORT,1194,DIRECT                            # OpenVPN
  - DST-PORT,500,DIRECT                             # IPSec IKE(UDP 500)
  - DST-PORT,4500,DIRECT                            # IPSec NAT-T(UDP 4500)
  - DST-PORT,1701,DIRECT                            # L2TP(UDP 1701)
  - DST-PORT,1723,DIRECT                            # PPTP(TCP 1723)
  - DST-PORT,5353,DIRECT                            # mDNS(局域网发现)
  - DST-PORT,123,DIRECT                             # NTP(时间同步)
  - DST-PORT,161,DIRECT                             # SNMP(网管)
  - DST-PORT,1883,DIRECT                            # MQTT(IoT)
  - DST-PORT,8883,DIRECT                            # MQTT over TLS
  - DST-PORT,5683,DIRECT                            # CoAP(IoT)
  - DST-PORT,3260,DIRECT                            # iSCSI(IP 存储)
  - DST-PORT,3306,DIRECT                            # MySQL
  - DST-PORT,5432,DIRECT                            # PostgreSQL
  - DST-PORT,6379,DIRECT                            # Redis
  - DST-PORT,27017,DIRECT                           # MongoDB
  - DST-PORT,873,DIRECT                             # Rsync
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
