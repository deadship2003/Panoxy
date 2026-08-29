# 流量策略

## 核心原则

> **不阻断任何协议;透明代理的第一目标是正常访问,分流只是优化走向。**

## DNS 处理

| 协议 | 端口 | 处理 | 效果 |
|---|---|---|---|
| 普通 DNS | UDP/TCP 53 | **劫持** → mihomo:1053 | fake-ip 模式,域名级分流精确 |
| DoT | TCP 853 | 正常分流 | 加密 DNS 走代理(为自定义设备保留访问) |
| DoQ | UDP 853 | 正常分流 | 同上 |
| DoH | TCP 443 | 正常分流 | 与 HTTPS 同端口,无法也不应阻断 |
| QUIC/HTTP3 | UDP 443 | 正常分流 | HTTP/3 原生体验;SNI 加密,域名规则不生效 |

## Tailscale 排除

| 项目 | 值 | 排除方式 |
|---|---|---|
| 直连 UDP | 41641 | 模板 DST-PORT + 防火墙 tproxy 链 |
| STUN/TURN | 3478 | 同上 |
| MagicDNS | 100.100.100.100:53 | 防火墙 DNS 链 + 模板 IP-CIDR |
| CGNAT 子网 | 100.64.0.0/10 | keep4 集合 + 模板 IP-CIDR |
| 接口 | tailscale0 | 防火墙 DNS 链 + tproxy 链 iifname 排除 |

## 基础服务直连(32 条)

### 远程管理
| 端口 | 服务 | 原因 |
|---|---|---|
| 22 | SSH/SFTP | 延迟敏感,走代理增加延迟 |
| 23 | Telnet | 同上 |

### 远程桌面
| 端口 | 服务 | 原因 |
|---|---|---|
| 3389 | RDP | 延迟敏感(画面卡顿) |
| 5900 | VNC | 同上 |

### VPN / 组网
| 端口 | 服务 | 原因 |
|---|---|---|
| 41641 | Tailscale | UDP 打洞,走代理 P2P 失败 |
| 3478 | STUN/TURN | NAT 穿透 |
| 51820 | WireGuard | UDP 封装,走代理严重延迟 |
| 1194 | OpenVPN | 同上 |
| 500 | IPSec IKE | UDP 协商,走代理隧道建立失败 |
| 4500 | IPSec NAT-T | NAT 穿透后的 IPSec |
| 1701 | L2TP | UDP 封装,走代理严重延迟 |
| 1723 | PPTP | TCP/GRE,走代理握手失败 |

### VoIP / 语音
| 端口 | 服务 | 原因 |
|---|---|---|
| 5060 | SIP | 语音信令,延迟 = 通话质量差 |
| 5061 | SIPS | SIP over TLS |

### 域认证 / 目录
| 端口 | 服务 | 原因 |
|---|---|---|
| 88 | Kerberos | AD 域登录,走代理可能认证失败 |
| 389 | LDAP | 目录查询 |
| 636 | LDAPS | LDAP over TLS |
| 1812 | RADIUS | 网络认证(WiFi 等) |
| 1813 | RADIUS 计费 | 同上 |

### 发现 / 时间 / 网管
| 端口 | 服务 | 原因 |
|---|---|---|
| 5353 | mDNS/DNS-SD | 局域网设备发现,走代理发现不到 |
| 123 | NTP | 时间同步 |
| 161 | SNMP | 网管监控 |
| 1900 | SSDP/UPnP | 智能设备发现 |

### IoT / 智能家居
| 端口 | 服务 | 原因 |
|---|---|---|
| 1883 | MQTT | Home Assistant 等 IoT 通信 |
| 8883 | MQTT/TLS | 同上 |
| 5683 | CoAP | 轻量 IoT 协议 |

### 存储 / 数据库
| 端口 | 服务 | 原因 |
|---|---|---|
| 3260 | iSCSI | IP 存储,延迟极其敏感 |
| 3306 | MySQL | 主从同步 |
| 5432 | PostgreSQL | 同上 |
| 6379 | Redis | 缓存同步 |
| 27017 | MongoDB | 副本集 |
| 873 | Rsync | 文件同步 |

## 实现位置

| 层级 | 文件 | 生效范围 |
|---|---|---|
| mihomo 规则引擎 | `src/internal/asset/config.tpl` rules 段 | TUN + TPROXY |
| nftables DNS 劫持 | `src/internal/firewall/rules.go` dns_prerouting/dns_output | TUN + TPROXY |
| nftables tproxy 链 | `src/internal/firewall/rules.go` tproxy_prerouting | 仅 TPROXY |
