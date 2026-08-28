package firewall

import "fmt"

// iptables 后端规则命令构建(纯函数,黄金单测覆盖)。
// 链命名:PANIXY_DNS(nat)/PANIXY_DOT(filter)/PANIXY_TP(mangle);ip6tables 同名加 _6 语义。
// 兼容性:REDIRECT 在 nat OUTPUT/PREROUTING 均为端口重定向,目标语义与 nft redirect 一致。

// BuildIptDnsCmds 生成 DNS 劫持(先清理后加载,幂等)。
func BuildIptDnsCmds(dnsPort, markSelf int) []string {
	var c []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		c = append(c,
			// nat:DNS 劫持链
			fmt.Sprintf("%s -t nat -N PANIXY_DNS", bin),
			fmt.Sprintf("%s -t nat -F PANIXY_DNS", bin),
			fmt.Sprintf("%s -t nat -A PANIXY_DNS -m mark --mark %d -j RETURN", bin, markSelf),
			fmt.Sprintf("%s -t nat -A PANIXY_DNS -p udp --dport 53 -j REDIRECT --to-ports %d", bin, dnsPort),
			fmt.Sprintf("%s -t nat -A PANIXY_DNS -p tcp --dport 53 -j REDIRECT --to-ports %d", bin, dnsPort),
			fmt.Sprintf("%s -t nat -C OUTPUT -j PANIXY_DNS || %s -t nat -A OUTPUT -j PANIXY_DNS", bin, bin),
			fmt.Sprintf("%s -t nat -C PREROUTING -j PANIXY_DNS || %s -t nat -A PREROUTING -j PANIXY_DNS", bin, bin),
		)
	}
	// v4 专属:OUTPUT 里先放行保留网段(防内网 DNS 异常与回环)
	v4 := []string{
		"iptables -t nat -I PANIXY_DNS 1 -d 10.0.0.0/8 -j RETURN",
		"iptables -t nat -I PANIXY_DNS 2 -d 172.16.0.0/12 -j RETURN",
		"iptables -t nat -I PANIXY_DNS 3 -d 192.168.0.0/16 -j RETURN",
		"iptables -t nat -I PANIXY_DNS 4 -d 127.0.0.0/8 -j RETURN",
	}
	return append(c, v4...)
}

// BuildIptTproxyCmds 生成 TPROXY 附加规则(mangle)。
func BuildIptTproxyCmds(markSelf, markTproxy, tproxyPort int) []string {
	var c []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		c = append(c,
			fmt.Sprintf("%s -t mangle -N PANIXY_TP", bin),
			fmt.Sprintf("%s -t mangle -F PANIXY_TP", bin),
			fmt.Sprintf("%s -t mangle -A PANIXY_TP -m mark --mark %d -j RETURN", bin, markSelf),
			fmt.Sprintf("%s -t mangle -A PANIXY_TP -p udp --dport 53 -j RETURN", bin),
			fmt.Sprintf("%s -t mangle -A PANIXY_TP -p tcp --dport 53 -j RETURN", bin),
			fmt.Sprintf("%s -t mangle -A PANIXY_TP -p tcp -j TPROXY --on-port %d --tproxy-mark %d", bin, tproxyPort, markTproxy),
			fmt.Sprintf("%s -t mangle -A PANIXY_TP -p udp -j TPROXY --on-port %d --tproxy-mark %d", bin, tproxyPort, markTproxy),
			fmt.Sprintf("%s -t mangle -C PREROUTING -j PANIXY_TP || %s -t mangle -A PREROUTING -j PANIXY_TP", bin, bin),
		)
	}
	return c
}

// BuildIptCleanCmds 生成清理命令(幂等;-w 串行防并发)。
func BuildIptCleanCmds() []string {
	var c []string
	for _, bin := range []string{"iptables", "ip6tables"} {
		c = append(c,
			fmt.Sprintf("%s -t nat -D OUTPUT -j PANIXY_DNS", bin),
			fmt.Sprintf("%s -t nat -D PREROUTING -j PANIXY_DNS", bin),
			fmt.Sprintf("%s -t nat -F PANIXY_DNS", bin),
			fmt.Sprintf("%s -t nat -X PANIXY_DNS", bin),
			fmt.Sprintf("%s -t mangle -D PREROUTING -j PANIXY_TP", bin),
			fmt.Sprintf("%s -t mangle -F PANIXY_TP", bin),
			fmt.Sprintf("%s -t mangle -X PANIXY_TP", bin),
		)
	}
	return c
}
