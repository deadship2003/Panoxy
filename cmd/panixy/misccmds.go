package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/config"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/systemdunit"
	"time"
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
	if err := needRoot(); err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("用法: panixy apply-conf <yaml>")
	}
	p := paths.Get()
	src := args[0]
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("文件不存在: %s", src)
	}
	if out, err := mihomoTest(p, src); err != nil {
		return fmt.Errorf("该文件未通过内核校验(%s),系统未做任何改动", firstErrLine(out))
	}
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
		config.Restore(p.Conf)
		systemdunit.Restart()
		return fmt.Errorf("重启失败,已恢复原配置")
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		config.Restore(p.Conf)
		systemdunit.Restart()
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
