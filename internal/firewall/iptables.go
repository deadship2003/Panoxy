package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/logx"
)

// iptBackend:nft 不可用时的 iptables/ip6tables 兜底。
// 自有链: nat 的 PANIXY_DNS(_6) 挂 OUTPUT/PREROUTING;filter 的 PANIXY_DOT(_6) 挂双钩子。
// TPROXY 模式另加 mangle 的 PANIXY_TP(_6) + 策略路由(见 policy.go)。

type iptBackend struct{}

func (i *iptBackend) Name() string { return "iptables" }

func ipt(line string) error {
	out, err := exec.Command("sh", "-c", line).CombinedOutput()
	logx.DebugCmd("sh", []string{"-c", line}, string(out), err)
	if err != nil && !iptTolerant(out) {
		return fmt.Errorf("%s 失败: %s", line, strings.TrimSpace(string(out)))
	}
	return nil
}

func ipt6(line string) error {
	out, err := exec.Command("sh", "-c", line).CombinedOutput()
	logx.DebugCmd("sh", []string{"-c", line}, string(out), err)
	if err != nil && !iptTolerant(out) {
		return fmt.Errorf("%s 失败: %s", line, strings.TrimSpace(string(out)))
	}
	return nil
}

func iptTolerant(out []byte) bool {
	s := string(out)
	return strings.Contains(s, "does not exist") || strings.Contains(s, "Too many links")
}

func (i *iptBackend) CleanAll() error {
	for _, cmd := range BuildIptCleanCmds() {
		if err := runIptLine(cmd); err != nil {
			return err
		}
	}
	if err := TproxyPolicyDel(); err != nil {
		return err
	}
	logx.Step("防火墙:已清理自有 iptables 链与策略路由(含残留)")
	return nil
}

func (i *iptBackend) ApplyDnsHijack() error {
	if err := i.CleanAll(); err != nil {
		return err
	}
	for _, cmd := range BuildIptDnsCmds(constants.DnsListenPort, constants.MarkSelf) {
		if err := runIptLine(cmd); err != nil {
			return err
		}
	}
	logx.Info("防火墙:iptables 后端已加载 DNS 劫持")
	return nil
}

func (i *iptBackend) ApplyTproxy() error {
	if err := i.ApplyDnsHijack(); err != nil {
		return err
	}
	for _, cmd := range BuildIptTproxyCmds(constants.MarkSelf, constants.MarkTproxy, constants.TproxyPort) {
		if err := runIptLine(cmd); err != nil {
			return err
		}
	}
	return TproxyPolicyAdd()
}

func (i *iptBackend) Teardown() error { return i.CleanAll() }

func (i *iptBackend) HasStaleRules() (bool, error) {
	for _, args := range [][]string{
		{"-t", "nat", "-nL", "PANIXY_DNS"},
		{"-t", "mangle", "-nL", "PANIXY_TP"},
	} {
		out, err := exec.Command("iptables", args...).CombinedOutput()
		if err == nil && !strings.Contains(string(out), "No chain") {
			return true, nil
		}
	}
	return false, nil
}

// runIptLine 按前缀分发到 iptables/ip6tables(命令为常量生成,无注入面)。
func runIptLine(cmd string) error {
	if strings.HasPrefix(cmd, "ip6tables ") {
		return ipt6(cmd)
	}
	return ipt(cmd)
}
