package firewall

import (
	"fmt"
	"strings"
)

// BuildNftScript 生成 TUN 模式的完整 nft 脚本(纯函数,便于黄金单测)。
// 结构:一张独立表 inet panixy:
//   - 保留网段集合(OUTPUT 放行,防内网/回环 DNS 被劫持与环路)
//   - prerouting(nat):LAN 客户端 53 → redirect :1053(落入接口主地址,v4/v6 通吃)
//   - output(nat):本机 53 → redirect :1053(落 127.0.0.1);
//     先放行 保留网段 与 mark=markSelf(mihomo 自身上游,防回环)
//   - input(filter):放行 lo/内网 到 1053(防宿主防火墙默认 DROP 挡掉劫持流量)
//   - prerouting/output(filter):拒绝 853(DoT/DoQ;DoH 443 无法在内核劫持)
func BuildNftScript(dnsPort, markSelf int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `table inet panixy {
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
  chain dot_prerouting {
    type filter hook prerouting priority filter; policy accept;
    meta l4proto { tcp, udp } th dport 853 reject
  }
  chain dot_output {
    type filter hook output priority filter; policy accept;
    meta mark %d return
    meta l4proto { tcp, udp } th dport 853 reject
  }
}
`, dnsPort, markSelf, dnsPort, dnsPort, dnsPort, dnsPort, markSelf)
	return b.String()
}

// BuildNftTproxyScript 生成 TPROXY 模式脚本:在 TUN 版之上增加
// mangle 打标与 tproxy 投递,并放行 mark=markSelf 与保留网段。
// 策略路由(ip rule/route table)由 iptables/nft 外的 ip 命令维护,见 TproxyPolicy*。
func BuildNftTproxyScript(dnsPort, markSelf, markTproxy, table, tproxyPort int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `table inet panixy {
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
  chain dot_prerouting {
    type filter hook prerouting priority filter; policy accept;
    meta l4proto { tcp, udp } th dport 853 reject
  }
  chain dot_output {
    type filter hook output priority filter; policy accept;
    meta mark %d return
    meta l4proto { tcp, udp } th dport 853 reject
  }
  chain tproxy_prerouting {
    type filter hook prerouting priority mangle; policy accept;

    # ===== 基础服务直连(在 mark/tproxy 之前 return,保证 SSH/VPN 正常)=====
    iifname "tailscale0" return
    tcp dport { 22, 23 } return
    udp dport { 41641, 3478, 51820, 1194 } return
    udp dport { 5353, 123 } return

    # ===== 原有排除 =====
    iifname "lo" return
    ip daddr @keep4 return
    ip6 daddr @keep6 return
    meta mark %d return
    meta l4proto { tcp, udp } th dport 53 return
    meta nfproto ipv4 meta l4proto { tcp, udp } tproxy ip to :%d meta mark set %d accept
    meta nfproto ipv6 meta l4proto { tcp, udp } tproxy ip6 to :%d meta mark set %d accept
  }
}
`, dnsPort, markSelf, dnsPort, dnsPort, dnsPort, dnsPort, markSelf,
		markSelf, tproxyPort, markTproxy, tproxyPort, markTproxy)
	return b.String()
}
