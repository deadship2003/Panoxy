package firewall

import (
	"fmt"

	"github.com/deadship2003/Panoxy/internal/constants"
)

// iptables 后端规则命令构建(纯函数,黄金单测覆盖)。
// 链命名随程序名派生:<PROG>_DNS(nat)/<PROG>_DOT(filter)/<PROG>_TP(mangle);ip6tables 同名。
// 兼容性:REDIRECT 在 nat OUTPUT/PREROUTING 均为端口重定向,目标语义与 nft redirect 一致。

// 链名随程序名派生(EnvPrefix: PANIXY_ → <PROG>_)。
var (
	chainDNS = constants.EnvPrefix() + "_DNS"
	chainTP  = constants.EnvPrefix() + "_TP"
)

// BuildIptDnsCmds 生成 DNS 劫持(先清理后加载,幂等)。
func BuildIptDnsCmds(dnsPort, markSelf int) []string {
	var c []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		c = append(c,
			// nat:DNS 劫持链
			fmt.Sprintf("%s -t nat -N %s", bin, chainDNS),
			fmt.Sprintf("%s -t nat -F %s", bin, chainDNS),
			fmt.Sprintf("%s -t nat -A %s -m mark --mark %d -j RETURN", bin, chainDNS, markSelf),
			fmt.Sprintf("%s -t nat -A %s -p udp --dport 53 -j REDIRECT --to-ports %d", bin, chainDNS, dnsPort),
			fmt.Sprintf("%s -t nat -A %s -p tcp --dport 53 -j REDIRECT --to-ports %d", bin, chainDNS, dnsPort),
			fmt.Sprintf("%s -t nat -C OUTPUT -j %s || %s -t nat -A OUTPUT -j %s", bin, chainDNS, bin, chainDNS),
			fmt.Sprintf("%s -t nat -C PREROUTING -j %s || %s -t nat -A PREROUTING -j %s", bin, chainDNS, bin, chainDNS),
		)
	}
	// v4 专属:OUTPUT 里先放行保留网段(防内网 DNS 异常与回环)
	v4 := []string{
		fmt.Sprintf("iptables -t nat -I %s 1 -d 10.0.0.0/8 -j RETURN", chainDNS),
		fmt.Sprintf("iptables -t nat -I %s 2 -d 172.16.0.0/12 -j RETURN", chainDNS),
		fmt.Sprintf("iptables -t nat -I %s 3 -d 192.168.0.0/16 -j RETURN", chainDNS),
		fmt.Sprintf("iptables -t nat -I %s 4 -d 127.0.0.0/8 -j RETURN", chainDNS),
	}
	return append(c, v4...)
}

// BuildIptTproxyCmds 生成 TPROXY 附加规则(mangle)。
func BuildIptTproxyCmds(markSelf, markTproxy, tproxyPort int) []string {
	var c []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		c = append(c,
			fmt.Sprintf("%s -t mangle -N %s", bin, chainTP),
			fmt.Sprintf("%s -t mangle -F %s", bin, chainTP),
			fmt.Sprintf("%s -t mangle -A %s -m mark --mark %d -j RETURN", bin, chainTP, markSelf),
			fmt.Sprintf("%s -t mangle -A %s -p udp --dport 53 -j RETURN", bin, chainTP),
			fmt.Sprintf("%s -t mangle -A %s -p tcp --dport 53 -j RETURN", bin, chainTP),
			fmt.Sprintf("%s -t mangle -A %s -p tcp -j TPROXY --on-port %d --tproxy-mark %d", bin, chainTP, tproxyPort, markTproxy),
			fmt.Sprintf("%s -t mangle -A %s -p udp -j TPROXY --on-port %d --tproxy-mark %d", bin, chainTP, tproxyPort, markTproxy),
			fmt.Sprintf("%s -t mangle -C PREROUTING -j %s || %s -t mangle -A PREROUTING -j %s", bin, chainTP, bin, chainTP),
		)
	}
	return c
}

// BuildIptCleanCmds 生成清理命令(幂等;-w 串行防并发)。
func BuildIptCleanCmds() []string {
	var c []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		c = append(c,
			fmt.Sprintf("%s -t nat -D OUTPUT -j %s", bin, chainDNS),
			fmt.Sprintf("%s -t nat -D PREROUTING -j %s", bin, chainDNS),
			fmt.Sprintf("%s -t nat -F %s", bin, chainDNS),
			fmt.Sprintf("%s -t nat -X %s", bin, chainDNS),
			fmt.Sprintf("%s -t mangle -D PREROUTING -j %s", bin, chainTP),
			fmt.Sprintf("%s -t mangle -F %s", bin, chainTP),
			fmt.Sprintf("%s -t mangle -X %s", bin, chainTP),
		)
	}
	return c
}
