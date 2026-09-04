package tproxy

// panoxy 裁剪(M3):mihomo 原生 iptables 透明代理规则生成器已移除。
//
// panoxy 用自研 nftables 规则 + 策略路由实现透明代理(见 internal/firewall),
// config.tpl 里 iptables.enable 恒为 false,因此 mihomo 自带的那套 iptables 写内核逻辑
// (SetTProxyIPTables 的 mangle/nat 链、CleanupTProxyIPTables 的清理)是死代码,在此裁剪减体积。
//
// 保留两个函数签名(no-op),是因为 hub/executor 仍无条件引用它们(updateIPTables / Shutdown):
// 签名不变,executor 无需改动,子树 diff 最小化。若上游 subtree pull 重写本文件,
// 按此说明重新裁剪即可(重新删除 iptables 生成逻辑、保留空签名)。

// SetTProxyIPTables 是 no-op:mihomo 原生 iptables 透明代理已由 panoxy nftables 取代。
func SetTProxyIPTables(ifname string, bypass []string, tport uint16, dnsredir bool, dport uint16) error {
	return nil
}

// CleanupTProxyIPTables 是 no-op:mihomo 原生从不写内核,无需清理 iptables。
func CleanupTProxyIPTables() {}
