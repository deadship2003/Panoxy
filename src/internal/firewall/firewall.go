// Package firewall 管理 panixy 自有防火墙规则(DNS 劫持;TPROXY 模式另含打标/策略路由)。
//
// 设计要点:
//   - 独立表 inet <程序名>(= constants.NftTable,随编译期 ProgName 注入),绝不复用系统 nat/filter 表 → CleanAll = 删整表,幂等
//   - 启动无条件 CleanAll 再 Apply → kill -9/OOM 残留随 systemctl restart 自愈
//   - 本机 OUTPUT 劫持排除:保留网段/回环(防内网 DNS 异常)+ mark 6666(mihomo 自身
//     上游查询,防 DNS 回环死锁 —— 与配置模板 routing-mark 联动)
//   - DNS 劫持用 redirect(mihomo 监听 0.0.0.0:1053):OUTPUT 落 127.0.0.1,
//     PREROUTING 落入接口主地址,v4/v6 通吃,无需 route_localnet
//   - 853(DoT/DoQ)拒绝:DoH(443)无法在内核劫持,由 status 提示用户关闭浏览器内置 DoH
package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/logx"
)

// Firewall 是防火墙后端抽象(spec 六 核心接口,加 ApplyTproxy/Name)。
type Firewall interface {
	Name() string
	CleanAll() error              // 无条件删除自有全部规则(启动第一步)
	ApplyDnsHijack() error        // TUN 模式:仅 DNS 劫持 + 853 拒绝
	ApplyTproxy() error           // TPROXY 模式:DNS + mark/策略路由/tproxy 链
	Teardown() error              // 删除自有全部表/链/策略路由(停止信号)
	HasStaleRules() (bool, error) // 是否检测到残留规则
}

// New 按可用性选择后端:nftables 优先,降级 iptables。
func New() (Firewall, error) {
	if _, err := exec.LookPath("nft"); err == nil {
		return &nftBackend{}, nil
	}
	if _, err := exec.LookPath("iptables"); err == nil {
		return &iptBackend{}, nil
	}
	return nil, fmt.Errorf("system has neither nft nor iptables, cannot manage DNS hijacking")
}

// ---- nftables 后端 ----

type nftBackend struct{}

func (n *nftBackend) Name() string { return "nftables" }

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

func (n *nftBackend) CleanAll() error {
	// 无条件删表;表不存在视为成功(幂等)
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

func (n *nftBackend) ApplyDnsHijack() error {
	if err := n.CleanAll(); err != nil {
		return err
	}
	if err := runNft(BuildNftScript(constants.DnsListenPort, constants.MarkSelf)); err != nil {
		return err
	}
	logx.Info("firewall: nftables backend loaded DNS hijack")
	return nil
}

func (n *nftBackend) ApplyTproxy() error {
	if err := n.CleanAll(); err != nil {
		return err
	}
	if err := runNft(BuildNftTproxyScript(constants.DnsListenPort, constants.MarkSelf,
		constants.MarkTproxy, constants.TproxyTable, constants.TproxyPort)); err != nil {
		return err
	}
	if err := TproxyPolicyAdd(); err != nil {
		return err
	}
	logx.Info("firewall: nftables backend loaded full TPROXY rules")
	return nil
}

func (n *nftBackend) Teardown() error { return n.CleanAll() }

func (n *nftBackend) HasStaleRules() (bool, error) {
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

func isNotExist(out []byte, err error) bool {
	s := string(out)
	if err != nil {
		s += err.Error()
	}
	return strings.Contains(s, "No such file or directory") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "No such device")
}
