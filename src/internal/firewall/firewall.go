// Package firewall 管理 panixy 自有防火墙规则(DNS 劫持;TPROXY 模式另含打标/策略路由)。
//
// 设计要点:
//   - 独立表 inet <程序名>(= constants.NftTable,随编译期 ProgName 注入),绝不复用系统 nat/filter 表 → CleanAll = 删整表,幂等
//   - 启动无条件 CleanAll 再 Apply → kill -9/OOM 残留随 systemctl restart 自愈
//   - 本机 OUTPUT 劫持排除:保留网段/回环(防内网 DNS 异常)+ mark 6666(mihomo 自身
//     上游查询,防 DNS 回环死锁 —— 与配置模板 routing-mark 联动)
//   - DNS 劫持用 redirect(mihomo 监听 [::]:1053 双栈):OUTPUT 落 127.0.0.1,
//     PREROUTING 落入接口主地址,v4/v6 通吃,无需 route_localnet
//   - 唯一后端 nftables(内核 4.18+ 均含 nf_tproxy 支持),不保留 iptables 兜底
package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/logx"
)

// BackendName 是唯一防火墙后端名(健康报告展示用)。
const BackendName = "nftables"

// ensureNft 校验 nftables 用户态可用;Panoxy 只支持 nftables,缺失即快速失败并给出安装提示。
func ensureNft() error {
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("nftables not found: Panoxy requires the nftables userspace (install the 'nftables' package)")
	}
	return nil
}

func runNft(script string) error {
	c := exec.Command("nft", "-f", "-")
	c.Stdin = strings.NewReader(script)
	out, err := c.CombinedOutput()
	logx.DebugCmd("nft", []string{"-f", "-"}, string(out), err)
	if err != nil {
		return fmt.Errorf("nft failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// CleanAll 无条件删除自有表 + 策略路由(启动第一步;表不存在视为成功,幂等)。
func CleanAll() error {
	if err := ensureNft(); err != nil {
		return err
	}
	args := []string{"delete", "table", constants.NftFamily, constants.NftTable}
	out, err := exec.Command("nft", args...).CombinedOutput()
	logx.DebugCmd("nft", args, string(out), err)
	if err != nil && !isNotExist(out, err) {
		return fmt.Errorf("failed to clean old rules: %s", strings.TrimSpace(string(out)))
	}
	if err := TproxyPolicyDel(); err != nil {
		return err
	}
	logx.Step("firewall: cleaned own table %s %s and policy routing (including kill -9 residue)", constants.NftFamily, constants.NftTable)
	return nil
}

// ApplyDnsHijack TUN 模式:仅 DNS 劫持(先 CleanAll 再加载,幂等)。
func ApplyDnsHijack() error {
	if err := CleanAll(); err != nil {
		return err
	}
	if err := runNft(BuildNftScript(constants.DnsListenPort, constants.MarkSelf)); err != nil {
		return err
	}
	logx.Info("firewall: loaded DNS hijack")
	return nil
}

// ApplyTproxy TPROXY 模式:DNS + mark/策略路由/tproxy 链(先 CleanAll 再加载,幂等)。
func ApplyTproxy() error {
	if err := CleanAll(); err != nil {
		return err
	}
	if err := runNft(BuildNftTproxyScript(constants.DnsListenPort, constants.MarkSelf,
		constants.MarkTproxy, constants.TproxyTable, constants.TproxyPort)); err != nil {
		return err
	}
	if err := TproxyPolicyAdd(); err != nil {
		return err
	}
	logx.Info("firewall: loaded full TPROXY rules")
	return nil
}

// Teardown 删除自有全部规则(停止信号)。
func Teardown() error { return CleanAll() }

// HasStaleRules 表存在即视为有残留规则。
func HasStaleRules() (bool, error) {
	if err := ensureNft(); err != nil {
		return false, err
	}
	args := []string{"list", "table", constants.NftFamily, constants.NftTable}
	out, err := exec.Command("nft", args...).CombinedOutput()
	logx.DebugCmd("nft", args, string(out), err)
	if err != nil {
		if isNotExist(out, err) {
			return false, nil
		}
		return false, fmt.Errorf("nft list failed: %s", strings.TrimSpace(string(out)))
	}
	return true, nil
}

// CheckTproxySupport 用最小 tproxy 规则做 nft -c 干跑:一次校验用户态语法 + 内核 nf_tproxy 支持。
// nftables TPROXY 走 inet 族 `tproxy to :port` 语句(依赖 nf_tproxy_ipv4/ipv6 模块)。
func CheckTproxySupport() error {
	if err := ensureNft(); err != nil {
		return err
	}
	script := fmt.Sprintf(`table inet %s_tproxy_probe {
  chain tproxy_probe {
    type filter hook prerouting priority mangle; policy accept;
    meta l4proto { tcp, udp } tproxy to :%d
  }
}
`, constants.NftTable, constants.TproxyPort)
	c := exec.Command("nft", "-c", "-f", "-")
	c.Stdin = strings.NewReader(script)
	out, err := c.CombinedOutput()
	logx.DebugCmd("nft", []string{"-c", "-f", "-"}, string(out), err)
	if err != nil {
		return fmt.Errorf("nftables cannot express tproxy: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func isNotExist(out []byte, err error) bool {
	s := string(out)
	if err != nil {
		s += err.Error()
	}
	return strings.Contains(s, "No such file or directory") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "No such device")
}
