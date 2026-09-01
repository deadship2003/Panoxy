# 技术笔记

两个核心机制的深入分析,面向排查与二次开发——不是历史变更记录。

## 一、TPROXY 模式:本机流量闭环与故障恢复

### 1. 现象

- TUN 模式正常;`sudo Panoxy mode tproxy` 切换后,**网关本机**上的应用连不上外网。
- LAN 客户端通常仍正常(它们走 PREROUTING 的另一条路径),因此容易被误判成「只有某个应用坏了」,而非防火墙模型问题。

### 2. 根因

TPROXY 默认只在 **prerouting** 钩子生效,抓不到网关本机 **output** 方向的流量。本机 DNS 被劫持后拿到 fake-ip(`198.18.0.1/16`),而 TPROXY 模式没有 TUN 设备去捕获该网段 → fake-ip 按默认路由被甩给网关后被黑洞 → 本机整体断网(国内外全断)。

与 TUN 等价,必须补全本机流量的 **闭环三要素**:

1. **打标** — `local_output` 链(`type route hook output priority mangle`,必须是 `type route` 才会触发 fwmark 重路由)把本机非 keep-out 的 tcp/udp 打 `mark 0x1`。
2. **回环重入** — 策略路由 `ip rule fwmark 1 lookup 100` + `ip route add local 0.0.0.0/0 dev lo table 100`,把被打标的本机流量重新送入内核,再进 prerouting tproxy。
3. **放行重入** — 移除 tproxy_prerouting 里的 `iifname "lo" return`(否则吞掉回环重入的包);回环目标地址由 keep4/keep6 兜底。

另保留 **DIVERT** 优化:`socket transparent 1 meta mark set 1 accept` 置于 tproxy 语句之前,处理已建立透明连接回环重入的后续包(内核 `tproxy.txt` 标准做法)。

### 3. 判定

- 防火墙规则是否含本机闭环:有无 `local_output` 链、有无 `iifname "lo" return`、策略路由 `fwmark 1 lookup 100` 是否就位。
- 部署的二进制是否与源码一致(源码已含修复而二进制未重编,是最常见的中招形态)。

### 4. 恢复流程

1. 确认源码已含闭环修复;分支落后则先合并对应提交。
2. 备份旧二进制 → 重编 → 重部署(静态二进制可直接拷贝覆盖)。
3. `sudo Panoxy mode tproxy` 后实测:本机出站走通 + 国内直连 + LAN 侧回归。
4. 保持 tproxy,或 `sudo Panoxy mode tun` 切回。

### 5. 关键参照

- `internal/firewall/rules.go` → `BuildNftTproxyScript`(`local_output` 链 + `tproxy_prerouting`)
- `internal/firewall/policy.go` → `tproxyPolicyCmds`(策略路由)
- `internal/constants/constants.go` → `MarkTproxy=1`、`TproxyTable=100`、`TproxyPort=7893`、`MarkSelf=6666`
- `internal/asset/config.tpl` → `.TProxy` 为真时输出 `tproxy-port: 7893` 并省略 `tun` 块

## 二、双栈 Fake-IP 设计

### 1. 目标

开启 IPv6 fake-ip,并把 DNS 监听从 v4-only 改成双栈,使 IPv6 与 IPv4 一样走 fake-ip 分流。

### 2. 为什么 IPv6 之前走不通

- fake-ip 模式下未配置 `fake-ip-range6` 时,AAAA 查询返回空(客户端回退 v4)。
- 旧 `dns.listen: 0.0.0.0:1053` 只绑 v4,v6 传输的 DNS 查询到不了内核。

### 3. 关键设计

- `dns.listen: "[::]:1053` 双栈:该字段是**单地址**、不支持逗号多地址;双栈靠 `[::]` + 内核 `net.ipv6.bindv6only=0`(v4 走 v4-mapped)。
- `fake-ip-range: 198.18.0.1/16`(v4)、`fake-ip-range6: 2001:2::1/48`(v6,RFC 5180 基准测试段,公网不可路由)。
- `keep6` 白名单**不得**包含 `2001:2::/48`(否则被当直连放行);选段在 ULA(`fc00::/7`)之外,故 `keep6` 里的 `fc00::/7` 无需收窄。
- 地址池 \(2^{80}\),长期无需变更网段。

### 4. 关键参照

- `internal/firewall/rules.go` → `fakeIpv4Range`/`fakeIpv6Range` 常量(单一事实源,网络形式 `198.18.0.0/16`、`2001:2::/48`)
- `internal/asset/config.tpl` → `dns.listen`、`fake-ip-range`、`fake-ip-range6`
- `internal/config/merge.go` → `--dns mine` 时强制写回 `[::]:1053`(防止被 merge 覆盖回 v4)
- `cmd/Panoxy/misccmds.go` → `warnCompat` 的 `dns.listen` 一致性告警
- `internal/constants` → `DnsListenPort=1053`
