package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/deadship2003/panixy/internal/asset"
	"github.com/deadship2003/panixy/internal/config"
	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/systemdunit"
)

// runCheck 用内核 -t 校验配置(只读,免 root)。
func runCheck(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	f := p.Conf
	if len(args) > 0 {
		f = args[0]
	}
	if _, err := os.Stat(f); err != nil {
		return fmt.Errorf("文件不存在: %s", f)
	}
	out, err := mihomoTest(p, f)
	fmt.Print(out)
	return err
}

// runApplyConf 应用自定义配置:优先热重载(仅非 provider 改动!),失败退重启,再失败恢复。
func runApplyConf(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error { return runApplyConfBody(p, cmd, args) })
}

func runApplyConfBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("用法: panixy apply-conf <yaml>")
	}
	src := args[0]
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("文件不存在: %s", src)
	}
	if out, err := mihomoTest(p, src); err != nil {
		return fmt.Errorf("该文件未通过内核校验(%s),系统未做任何改动", firstErrLine(out))
	}
	warnCompat(src)
	if err := config.Backup(p.Conf); err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.Conf, b, 0o644); err != nil {
		config.Restore(p.Conf)
		return err
	}
	api := mihomoapi.NewFromConf(p.Conf)
	// 热重载不刷新 proxy-providers(mihomo 限制);此处仅对非 provider 改动有效
	if err := api.ReloadConf(p.Conf); err == nil && health.WaitHealthy(p.Conf, 20*time.Second, "") == nil {
		config.ClearBackup(p.Conf)
		logx.Info("配置已热重载生效(未重启);注意 provider 改动需重启才生效")
		return nil
	}
	logx.Step("热重载未生效,改用重启方式")
	if err := systemdunit.Restart(); err != nil {
		rollbackRestart(p)
		return fmt.Errorf("重启失败,已恢复原配置")
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		rollbackRestart(p)
		return fmt.Errorf("重启后健康检查未过,已恢复原配置:%w", err)
	}
	config.ClearBackup(p.Conf)
	logx.Info("配置已生效: %s -> %s", src, p.Conf)
	return nil
}

// runUnits 输出渲染后的单元文本(离线审查,不动系统)。
func runUnits(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	units, err := systemdunit.Render(p, "tun")
	if err != nil {
		return err
	}
	for _, name := range []string{"panixy.service", "panixy-upgrade.service", "panixy-upgrade.timer"} {
		fmt.Printf("===== %s =====\n%s\n", name, units[name])
	}
	return nil
}

// runConfig 渲染默认模板打印到 stdout;--write 额外写回 config.default.yaml(不部署、不碰系统)。
func runConfig(cmd *cobra.Command, args []string) error {
	mode, _ := cmd.Flags().GetString("mode")
	if mode != "tun" && mode != "tproxy" {
		return fmt.Errorf("--mode 只能是 tun 或 tproxy")
	}
	secret, _ := cmd.Flags().GetString("secret")
	d := asset.DefaultConfigData()
	d.TProxy = mode == "tproxy"
	d.Secret = secret
	out, err := asset.RenderConfig(d)
	if err != nil {
		return err
	}
	fmt.Print(out)
	if write, _ := cmd.Flags().GetBool("write"); write {
		p := paths.Get()
		if err := writeDefaultConf(p, mode, secret); err != nil {
			return err
		}
		logx.Info("默认配置已写回 %s(模式 %s,密钥 %s)", p.DefaultConf, mode, secret)
	}
	return nil
}

// runLog 透传 journalctl。
func runLog(cmd *cobra.Command, args []string) error {
	n := "80"
	if len(args) > 0 {
		n = args[0]
	}
	out, err := journal(n)
	fmt.Print(out)
	return err
}

// warnCompat 个人配置融合前的兼容自检:与防火墙方案的三个"暗号"对不上会出实际问题。
//
//	routing-mark 6666 —— 防火墙据此放行 mihomo 自身流量,缺失会 DNS 回环
//	dns.listen 0.0.0.0:1053 —— nft redirect 的落点,不一致则 DNS 劫持落空
//	tun.dns-hijack —— 防火墙已统一做 DNS 劫持,保留会双重处理
func warnCompat(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c struct {
		RoutingMark int `yaml:"routing-mark"`
		DNS         struct {
			Listen string `yaml:"listen"`
		} `yaml:"dns"`
		TUN struct {
			DNSHijack []string `yaml:"dns-hijack"`
		} `yaml:"tun"`
	}
	if yaml.Unmarshal(b, &c) != nil {
		return
	}
	if c.RoutingMark != constants.MarkSelf {
		logx.Warn("配置缺 routing-mark: %d —— 防火墙将无法放行 mihomo 自身流量,可能造成 DNS 回环死锁(模板默认已带,勿删)", constants.MarkSelf)
	}
	if !strings.Contains(c.DNS.Listen, ":1053") {
		logx.Warn("dns.listen=%q 与防火墙劫持落点(0.0.0.0:1053)不一致 —— DNS 劫持将落空,请改为 0.0.0.0:1053", c.DNS.Listen)
	}
	if len(c.TUN.DNSHijack) > 0 {
		logx.Warn("tun.dns-hijack 仍存在 —— DNS 劫持已由防火墙统一处理,请删除该项避免双重劫持")
	}
}
