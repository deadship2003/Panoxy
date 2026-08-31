package firewall

import (
	"fmt"

	"github.com/deadship2003/Panoxy/internal/constants"
)

// keep-out 网段:内核层直接放行、绝不进 mihomo 的地址集合(与 TUN route-exclude 等价)。
// 刻意不含 fake-ip 段(见下方 fakeIpv4Range / fakeIpv6Range)—— 那是必须进 mihomo 才能还原域名,不能放行。
// 单一事实源:BuildNftScript / BuildNftTproxyScript 共用。
const (
	keep4Elements = "0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, " +
		"172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24, 192.168.0.0/16, " +
		"198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 240.0.0.0/4"
	// fc00::/7(ULA)在此放行。fake-ip6 段选在 RFC 5180 基准段 2001:2::/48(位于 ULA 之外),
	// 故无需收窄 fc00::/7;只需保证 2001:2::/48 不进 keep6(见 fakeIpv6Range 及测试断言)。
	keep6Elements = "::/128, ::1/128, 64:ff9b::/96, 100::/64, 2001:db8::/32, fc00::/7, fe80::/10, ff00::/8"

	// 端口级 keep-out:内核层直接放行的关键端口(SSH/Telnet/VPN/NAT/mDNS/NTP)。
	// 这是 config.tpl rules 段"基础服务直连"的内核级子集 —— 一旦这些端口被劫持 SSH/VPN 即断,
	// 故必须在此直接放行(其余基础服务端口由 mihomo rules 段直连)。两链共用,改动需同步。
	keepPortsTCP = "tcp dport { 22, 23 }"
	keepPortsUDP = "udp dport { 41641, 3478, 51820, 1194, 5353, 123 }"
)

// fake-ip 网段:必须进 mihomo 才能还原域名,故刻意不放进 keep 白名单。
// 单一事实源,与 config.tpl 的 fake-ip-range / fake-ip-range6 联动(此处为网段形式;config 用首地址形式)。
const (
	fakeIpv4Range = "198.18.0.0/16"
	fakeIpv6Range = "2001:2::/48" // RFC 5180 基准测试段(公网不可路由),IPv6 版 198.18.0.0/15
)

// BuildNftScript 生成 TUN 模式的完整 nft 脚本。
// 原则:不阻断任何协议(QUIC/DoT/DoQ/DoH 均纳入正常分流);正常访问优先于分流精度。
func BuildNftScript(dnsPort, markSelf int) string {
	return fmt.Sprintf(`table inet %s {
  set keep4 {
    type ipv4_addr
    flags interval
    elements = { %s }
  }
  set keep6 {
    type ipv6_addr
    flags interval
    elements = { %s }
  }
  chain dns_prerouting {
    type nat hook prerouting priority dstnat; policy accept;
    ip daddr 100.100.100.100 return
    iifname "tailscale0" return
    iifname != "lo" meta l4proto { tcp, udp } th dport 53 redirect to :%d
  }
  chain dns_output {
    type nat hook output priority dstnat; policy accept;
    ip daddr 100.100.100.100 return
    ip daddr @keep4 return
    ip6 daddr @keep6 return
    meta mark %d return
    meta l4proto { tcp, udp } th dport 53 redirect to :%d
  }
  chain dns_input {
    type filter hook input priority filter; policy accept;
    iifname "lo" th dport %d accept
    ip saddr @keep4 th dport %d accept
    ip6 saddr @keep6 th dport %d accept
  }
}
`, constants.NftTable, keep4Elements, keep6Elements,
		dnsPort, markSelf, dnsPort, dnsPort, dnsPort, dnsPort)
}

// BuildNftTproxyScript 生成 TPROXY 模式脚本:在 TUN 版之上增加 tproxy 链与本机输出打标链。
//
// 本机出站流量走 output 钩子(TPROXY 抓不到),故用 local_output 链把「非 keep-out 的本机
// tcp/udp 流量」打上 markTproxy,经策略路由 `ip rule fwmark 1 lookup 100` → `local 0.0.0.0/0
// dev lo` 回环重入,再走 tproxy_prerouting 交给 mihomo —— 与 TUN 等价(含 v6 与直连 IP)。
// 关键点:local_output 必须用 `type route`(强制 re-route,普通 output 钩子打 mark 不触发重路由)。
func BuildNftTproxyScript(dnsPort, markSelf, markTproxy, table, tproxyPort int) string {
	return fmt.Sprintf(`table inet %s {
  set keep4 {
    type ipv4_addr
    flags interval
    elements = { %s }
  }
  set keep6 {
    type ipv6_addr
    flags interval
    elements = { %s }
  }
  chain dns_prerouting {
    type nat hook prerouting priority dstnat; policy accept;
    ip daddr 100.100.100.100 return
    iifname "tailscale0" return
    iifname != "lo" meta l4proto { tcp, udp } th dport 53 redirect to :%d
  }
  chain dns_output {
    type nat hook output priority dstnat; policy accept;
    ip daddr 100.100.100.100 return
    ip daddr @keep4 return
    ip6 daddr @keep6 return
    meta mark %d return
    meta l4proto { tcp, udp } th dport 53 redirect to :%d
  }
  chain dns_input {
    type filter hook input priority filter; policy accept;
    iifname "lo" th dport %d accept
    ip saddr @keep4 th dport %d accept
    ip6 saddr @keep6 th dport %d accept
  }
  chain local_output {
    type route hook output priority mangle; policy accept;
    oifname "tailscale0" return
    %s return
    %s return
    ip daddr @keep4 return
    ip6 daddr @keep6 return
    meta mark != 0 return
    meta l4proto { tcp, udp } th dport 53 return
    meta l4proto { tcp, udp } meta mark set %d accept
  }
  chain tproxy_prerouting {
    type filter hook prerouting priority mangle; policy accept;
    iifname "tailscale0" return
    %s return
    %s return
    ip daddr @keep4 return
    ip6 daddr @keep6 return
    meta mark %d return
    meta l4proto { tcp, udp } th dport 53 return
    meta l4proto { tcp, udp } tproxy to :%d meta mark set %d accept
  }
}
`, constants.NftTable, keep4Elements, keep6Elements,
		dnsPort, markSelf, dnsPort, dnsPort, dnsPort, dnsPort,
		keepPortsTCP, keepPortsUDP, markTproxy,
		keepPortsTCP, keepPortsUDP, markSelf, tproxyPort, markTproxy)
}
