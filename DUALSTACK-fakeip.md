# Panoxy 双栈 Fake-IP 改造（IPv4 198.18.0.0/16 + IPv6 2001:2::/48）

> 供多主机、多分支同步/融合使用。改动已通过 `go build ./...` 与三个包的单测（含内嵌内核 `Panoxy check` 校验），**尚未部署到本机**。

## 0. 一句话

开启 mihomo 的 IPv6 fake-ip（`2001:2::/48`），并把 DNS 监听从 v4-only `0.0.0.0:1053` 改成双栈 `[::]:1053`。防火墙/策略路由的 IPv6 部分（`keep6Elements`、`ip6 daddr @keep6 return`、`ip -6 rule/route`）**本来就已就绪**，本次只是补上「让 mihomo 发 v6 假地址」这最后一块。

## 1. 为什么之前 IPv6 走不通

- mihomo 在 fake-ip 模式下，**未配置 `fake-ip-range6` 时 AAAA 查询返回空**（客户端回退 v4）。旧 config 只有 `fake-ip-range: 198.18.0.1/16`，没有 `fake-ip-range6`。
- 旧 `dns.listen: 0.0.0.0:1053` 只绑 v4，v6 传输的 DNS 查询到不了 mihomo。

## 2. 逐文件改动

### 2.1 `internal/firewall/rules.go`
- 新增单一事实源常量：
  ```go
  const (
      fakeIpv4Range = "198.18.0.0/16"
      fakeIpv6Range = "2001:2::/48" // RFC 5180 基准测试段(公网不可路由),IPv6 版 198.18.0.0/15
  )
  ```
- 修正过时注释：原「若启用 v6 fake-ip 须把 `fc00::/7` 收窄为 `fd00::/8`」已失效——选段 `2001:2::/48` 在 ULA 之外，**无需收窄**；只需保证 `2001:2::/48` 不进 `keep6`。
- `keep6Elements` 网段本身**不改**（`2001:2::/48` 本就不在其中）。

### 2.2 `internal/asset/config.tpl`
- `dns.listen`：`0.0.0.0:{{.DnsPort}}` → `"[::]:{{.DnsPort}}"`（双栈；v4 查询以 v4-mapped 形式接受，等价 `0.0.0.0`）。
- `dns` 段新增 `fake-ip-range6: 2001:2::1/48`。

### 2.3 `internal/config/merge.go`
- `--dns mine` 时强制写回 listen 的字面量 `0.0.0.0:1053` → `[::]:1053`（**否则会被 merge 覆盖回 v4**）。
- 相关注释同步。

### 2.4 `cmd/Panoxy/misccmds.go`
- `warnCompat` 的 `dns.listen` 一致性告警文案 `0.0.0.0:1053` → `[::]:1053`。判定逻辑仍是 `Contains(":1053")`，不受影响。

### 2.5 `internal/firewall/firewall.go`
- 顶部注释「mihomo 监听 0.0.0.0:1053」→「[::]:1053 双栈」。

### 2.6 测试
- `firewall_test.go`：新增 `keep6` 不得含 `fakeIpv6Range` 断言；v4 断言改用 `fakeIpv4Range` 常量。
- `asset_test.go`：`listen` 断言 → `[::]:1053`；新增 `fake-ip-range6: 2001:2::1/48` 断言。
- `merge_test.go`：基底保留断言 `listen` → `"[::]:1053"`。

## 3. 为什么选 `2001:2::/48`

- RFC 5180 基准测试段，公网不可路由，是 IPv4 基准段 `198.18.0.0/15` 的 IPv6 对偶（mihomo 默认 v4 就用 `198.18.0.1/16`）。
- 位于 ULA（`fc00::/7`）之外，不撞内网 ULA，故 `keep6` 里 `fc00::/7` 无需收窄。
- 地址池 \(2^{80}\)，长期无需变更网段。

## 4. 逐机验证（部署后执行）

```bash
# 编译机单测
go test ./internal/firewall/ ./internal/asset/ ./internal/config/

# 装到目标主机
cd /path/to/Panoxy && make build && sudo make install

# 渲染校验(重新渲染后)
grep -nE 'fake-ip-range6|listen:' /etc/Panoxy.yaml
#   期望:  fake-ip-range6: 2001:2::1/48    listen: "[::]:1053"

# 重载(触发重渲染+重启;若本机常驻 tproxy 用 mode 重挂一次即可)
sudo Panoxy mode tproxy      # 或 sudo Panoxy redeploy

# AAAA 返回 2001:2:: 前缀(v4 传输)
dig AAAA www.google.com @127.0.0.1 -p 1053 +short

# v6 传输的 DNS 查询(纯 v6 解析器路径,验证 [::] 监听)
dig AAAA www.google.com @::1 -p 1053 +short

# v6 实连走 fake-ip
curl -6 -sS -o /dev/null -w "HTTP=%{http_code} ip=%{remote_ip}\n" --max-time 10 https://www.google.com
#   期望: ip 是 2001:2:: 段

# 内网 ULA v6 直连不受影响
ip -6 route get fc00::1
```

## 5. 关键参照

- mihomo 键 `fake-ip-range6`（`config/dns.go` 的 `FakeIPRange6`；未配置时 AAAA 返回空）。
- `dns.listen` 是**单地址字段**，不支持逗号多地址；双栈靠 `"[::]"` + 内核 `net.ipv6.bindv6only=0`（v4 走 v4-mapped）。
- 网段常量：`internal/firewall/rules.go` → `fakeIpv4Range` / `fakeIpv6Range`。
- 端口常量：`internal/constants` → `DnsListenPort=1053`。
