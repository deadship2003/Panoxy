package firewall

import (
	"strings"
	"testing"

	"github.com/deadship2003/Panoxy/internal/constants"
)

// 黄金断言:规则文本的关键行必须存在 —— 这些行就是 DNS 劫持/防回环/853 拒绝的全部骨架。
func TestBuildNftScriptGolden(t *testing.T) {
	s := BuildNftScript(1053, 6666)
	for _, want := range []string{
		"table inet " + constants.NftTable + " {",
		"elements = {",
		"iifname != \"lo\" meta l4proto { tcp, udp } th dport 53 redirect to :1053",
		"ip daddr @keep4 return",
		"ip6 daddr @keep6 return",
		"meta mark 6666 return", // 内核自身放行(防 DNS 回环)
		"th dport 53 redirect to :1053",
		"ip daddr 100.100.100.100 return",          // Tailscale MagicDNS 不劫持
		"type nat hook prerouting priority dstnat", // PREROUTING:LAN 客户端
		"type nat hook output priority dstnat",     // OUTPUT:本机
	} {
		if !strings.Contains(s, want) {
			t.Errorf("nft 脚本缺少关键规则: %q", want)
		}
	}
	// 单一事实源:keep 集由常量注入,脚本须完整包含;且不得误含 fake-ip 段。
	if !strings.Contains(s, keep4Elements) {
		t.Errorf("keep4 缺少保留网段: %s", keep4Elements)
	}
	if !strings.Contains(s, keep6Elements) {
		t.Errorf("keep6 缺少保留网段: %s", keep6Elements)
	}
	if strings.Contains(keep4Elements, fakeIpv4Range) {
		t.Errorf("keep4 不得包含 fake-ip 段 %s(否则无法进内核还原域名)", fakeIpv4Range)
	}
	if strings.Contains(keep6Elements, fakeIpv6Range) {
		t.Errorf("keep6 不得包含 fake-ip6 段 %s(否则无法进内核还原域名)", fakeIpv6Range)
	}
	// 不阻断任何协议(DoT/DoQ 已移除阻断,纳入正常分流)
	if strings.Contains(s, "853 reject") {
		t.Errorf("不应阻断 853(DoT/DoQ 已纳入正常分流)")
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
		"meta l4proto { tcp, udp } tproxy to :7893 meta mark set 1 accept",
		// DIVERT 优化(内核 tproxy.txt 标准):已建立透明连接回环重入的后续包直接打标放行
		"meta l4proto { tcp, udp } socket transparent 1 meta mark set 1 accept",
		// 本机输出打标链(与 TUN 等价的关键):
		"chain local_output {",
		"type route hook output priority mangle", // 必须 type route,才触发 fwmark 重路由
		"meta mark != 0 return",                  // 内核自身(6666)与已打标(1)都不再碰
		"meta l4proto { tcp, udp } meta mark set 1 accept",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("tproxy 脚本缺少关键规则: %q", want)
		}
	}
	// 回环重入的本机流量必须能到达 tproxy,故不能再有 `iifname "lo" return`(会被 keep4/keep6 兜住)。
	if strings.Contains(s, `iifname "lo" return`) {
		t.Errorf("tproxy_prerouting 不应再有 iifname lo return(会吞掉回环重入的本机流量)")
	}
	// 回归:SSH(22)不得被内核级放行 —— config.tpl 已注释 DST-PORT,22,DIRECT(境外 SSH 走代理)。
	// 若内核级仍放行 22,TPROXY 模式下 SSH 永不进内核、GitHub SSH 直连被墙,与 TUN 行为不一致。
	if strings.Contains(keepPortsTCP, "22") {
		t.Errorf("keepPortsTCP 不得包含 22(SSH):应进内核分流,与 config.tpl 注释 DST-PORT,22,DIRECT 同步")
	}
	if strings.Contains(s, "dport { 22") {
		t.Errorf("TPROXY 脚本不应内核级放行 22 端口(SSH 应进内核分流)")
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

// TestTolerantError 真机首装实测教训:ip rule del 对不存在的规则报
// RTNETLINK ENOENT("No such file or directory"),漏容会导致 fw apply 失败、
// systemd 把服务判死。此为回归测试。
func TestTolerantError(t *testing.T) {
	for _, s := range []string{
		"RTNETLINK answers: No such file or directory",
		"RTNETLINK answers: File exists",
		"Error: ipv4: FIB rule does not exist",
	} {
		if !tolerantError(s) {
			t.Errorf("应容忍: %q", s)
		}
	}
	for _, s := range []string{
		"Operation not permitted",
		"memory allocation failure",
	} {
		if tolerantError(s) {
			t.Errorf("不应容忍: %q", s)
		}
	}
}
