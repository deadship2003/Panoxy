package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/logx"
)

// iptBackend:nft 不可用时的 iptables/ip6tables 兜底。
// 自有链: nat 的 PANIXY_DNS(_6) 挂 OUTPUT/PREROUTING;filter 的 PANIXY_DOT(_6) 挂双钩子。
// TPROXY 模式另加 mangle 的 PANIXY_TP(_6) + 策略路由(见 policy.go)。

type iptBackend struct{}

func (i *iptBackend) Name() string { return "iptables" }

// runIptCmd 执行一条 iptables/ip6tables 命令(命令由常量生成,无注入面)。
func runIptCmd(line string) error {
	out, err := exec.Command("sh", "-c", line).CombinedOutput()
	logx.DebugCmd("sh", []string{"-c", line}, string(out), err)
	if err != nil && !iptTolerant(out) {
		return fmt.Errorf("%s failed: %s", line, strings.TrimSpace(string(out)))
	}
	return nil
}

func iptTolerant(out []byte) bool {
	s := string(out)
	return tolerantError(s) || strings.Contains(s, "Too many links") ||
		strings.Contains(s, "by that name") || // No chain/target/match by that name
		strings.Contains(s, "Bad rule") // -D 删除不存在的规则
}

func (i *iptBackend) CleanAll() error {
	for _, cmd := range BuildIptCleanCmds() {
		if err := runIptCmd(cmd); err != nil {
			return err
		}
	}
	if err := TproxyPolicyDel(); err != nil {
		return err
	}
	logx.Step("firewall: cleaned own iptables chains and policy routing (including residue)")
	return nil
}

func (i *iptBackend) ApplyDnsHijack() error {
	if err := i.CleanAll(); err != nil {
		return err
	}
	for _, cmd := range BuildIptDnsCmds(constants.DnsListenPort, constants.MarkSelf) {
		if err := runIptCmd(cmd); err != nil {
			return err
		}
	}
	logx.Info("firewall: iptables backend loaded DNS hijack")
	return nil
}

func (i *iptBackend) ApplyTproxy() error {
	if err := i.ApplyDnsHijack(); err != nil {
		return err
	}
	for _, cmd := range BuildIptTproxyCmds(constants.MarkSelf, constants.MarkTproxy, constants.TproxyPort) {
		if err := runIptCmd(cmd); err != nil {
			return err
		}
	}
	return TproxyPolicyAdd()
}

func (i *iptBackend) Teardown() error { return i.CleanAll() }

func (i *iptBackend) HasStaleRules() (bool, error) {
	for _, args := range [][]string{
		{"-t", "nat", "-nL", chainDNS},
		{"-t", "mangle", "-nL", chainTP},
	} {
		out, err := exec.Command("iptables", args...).CombinedOutput()
		if err == nil && !strings.Contains(string(out), "No chain") {
			return true, nil
		}
	}
	return false, nil
}
