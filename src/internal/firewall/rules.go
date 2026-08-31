package firewall

import (
	"fmt"

	"github.com/deadship2003/Panoxy/internal/constants"
)

// BuildNftScript 生成 TUN 模式的完整 nft 脚本。
// 原则:不阻断任何协议(QUIC/DoT/DoQ/DoH 均纳入正常分流);正常访问优先于分流精度。
func BuildNftScript(dnsPort, markSelf int) string {
	return fmt.Sprintf(`table inet %s {
  set keep4 {
    type ipv4_addr
    flags interval
    elements = { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, 100.64.0.0/10 }
  }
  set keep6 {
    type ipv6_addr
    flags interval
    elements = { ::1/128, fe80::/10 }
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
`, constants.NftTable, dnsPort, markSelf, dnsPort, dnsPort, dnsPort, dnsPort)
}

// BuildNftTproxyScript 生成 TPROXY 模式脚本:在 TUN 版之上增加 tproxy 链。
func BuildNftTproxyScript(dnsPort, markSelf, markTproxy, table, tproxyPort int) string {
	return fmt.Sprintf(`table inet %s {
  set keep4 {
    type ipv4_addr
    flags interval
    elements = { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, 100.64.0.0/10, 224.0.0.0/4 }
  }
  set keep6 {
    type ipv6_addr
    flags interval
    elements = { ::1/128, fe80::/10, ff00::/8 }
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
  chain tproxy_prerouting {
    type filter hook prerouting priority mangle; policy accept;
    iifname "tailscale0" return
    tcp dport { 22, 23 } return
    udp dport { 41641, 3478, 51820, 1194 } return
    udp dport { 5353, 123 } return
    iifname "lo" return
    ip daddr @keep4 return
    ip6 daddr @keep6 return
    meta mark %d return
    meta l4proto { tcp, udp } th dport 53 return
    meta l4proto { tcp, udp } tproxy to :%d meta mark set %d accept
  }
}
`, constants.NftTable, dnsPort, markSelf, dnsPort, dnsPort, dnsPort, dnsPort,
		markSelf, tproxyPort, markTproxy)
}
