package firewall

import (
	"strings"
	"testing"

	"github.com/deadship2003/panixy/internal/constants"
)

// 黄金断言:规则文本的关键行必须存在 —— 这些行就是 DNS 劫持/防回环/853 拒绝的全部骨架。
func TestBuildNftScriptGolden(t *testing.T) {
	s := BuildNftScript(1053, 6666)
	for _, want := range []string{
		"table inet panixy {",
		`elements = { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, 100.64.0.0/10 }`,
		"iifname != \"lo\" meta l4proto { tcp, udp } th dport 53 redirect to :1053",
		"ip daddr @keep4 return",
		"ip6 daddr @keep6 return",
		"meta mark 6666 return",                    // mihomo 自身放行(防 DNS 回环)
		"th dport 53 redirect to :1053",
		"th dport 853 reject",                      // DoT/DoQ 拒绝
		"type nat hook prerouting priority dstnat", // PREROUTING:LAN 客户端
		"type nat hook output priority dstnat",     // OUTPUT:本机
	} {
		if !strings.Contains(s, want) {
			t.Errorf("nft 脚本缺少关键规则: %q", want)
		}
	}
	if strings.Contains(s, "127.0.0.1:1053") || strings.Contains(s, "dnat to 127.0.0.1") {
		t.Errorf("不应 DNAT 到 127.0.0.1(PREROUTING 场景不可达,应使用 redirect)")
	}
}

func TestBuildNftTproxyScriptGolden(t *testing.T) {
	s := BuildNftTproxyScript(1053, 6666, 1, 100, 7893)
	for _, want := range []string{
		"chain tproxy_prerouting {",
		"type filter hook prerouting priority mangle",
		"meta mark 6666 return",
		"th dport 53 return", // DNS 交给 nat 链,不进 tproxy
		"meta nfproto ipv4 meta l4proto { tcp, udp } tproxy ip to :7893 meta mark set 1 accept",
		"meta nfproto ipv6 meta l4proto { tcp, udp } tproxy ip6 to :7893 meta mark set 1 accept",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("tproxy 脚本缺少关键规则: %q", want)
		}
	}
}

func TestBuildIptCmdsGolden(t *testing.T) {
	dns := strings.Join(BuildIptDnsCmds(1053, 6666), "\n")
	for _, want := range []string{
		"iptables -t nat -N PANIXY_DNS",
		"iptables -t nat -A PANIXY_DNS -m mark --mark 6666 -j RETURN",
		"iptables -t nat -A PANIXY_DNS -p udp --dport 53 -j REDIRECT --to-ports 1053",
		"ip6tables -t nat -A PREROUTING -j PANIXY_DNS",
		"iptables -t nat -I PANIXY_DNS 1 -d 10.0.0.0/8 -j RETURN",
		"iptables -t filter -A PANIXY_DOT -p tcp --dport 853 -j REJECT",
	} {
		if !strings.Contains(dns, want) {
			t.Errorf("iptables DNS 命令缺少: %q", want)
		}
	}
	tp := strings.Join(BuildIptTproxyCmds(6666, 1, 7893), "\n")
	for _, want := range []string{
		"iptables -t mangle -N PANIXY_TP",
		"-j TPROXY --on-port 7893 --tproxy-mark 1",
		"ip6tables -t mangle -C PREROUTING -j PANIXY_TP || ip6tables -t mangle -A PREROUTING -j PANIXY_TP",
	} {
		if !strings.Contains(tp, want) {
			t.Errorf("iptables tproxy 命令缺少: %q", want)
		}
	}
	clean := strings.Join(BuildIptCleanCmds(), "\n")
	for _, want := range []string{
		"iptables -t nat -X PANIXY_DNS",
		"ip6tables -t mangle -X PANIXY_TP",
	} {
		if !strings.Contains(clean, want) {
			t.Errorf("清理命令缺少: %q", want)
		}
	}
}

func TestTproxyPolicyCmds(t *testing.T) {
	add := tproxyPolicyCmds(true, 1, 100)
	joined := strings.Join(flatten(add), " ")
	for _, want := range []string{"rule add fwmark 1 lookup 100", "route add local 0.0.0.0/0 dev lo table 100", "route add local ::/0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("策略路由缺少: %q", want)
		}
	}
	del := tproxyPolicyCmds(false, 1, 100)
	if j := strings.Join(flatten(del), " "); !strings.Contains(j, "rule del fwmark 1 lookup 100") {
		t.Errorf("清理缺少 rule del")
	}
}

func flatten(cmds [][]string) []string {
	var out []string
	for _, c := range cmds {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// TestConstantsInvariants 防呆:mark/端口等关键常量被意外改动会破坏与配置模板的联动。
func TestConstantsInvariants(t *testing.T) {
	if constants.MarkSelf != 6666 {
		t.Errorf("MarkSelf 必须与配置模板 routing-mark 联动(6666)")
	}
	if constants.DnsListenPort != 1053 {
		t.Errorf("DnsListenPort 必须与配置模板 dns.listen 联动(1053)")
	}
}
