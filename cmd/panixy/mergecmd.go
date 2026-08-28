package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/config"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/locker"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/systemdunit"
)

// runMergeConf 个人配置定向融合:基底(模式参数/暗号/基础设施)+ 个人(分组/规则/节点/端口密钥)。
func runMergeConf(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("用法: panixy merge-conf <个人配置.yaml>(字段级融合,非整体接管;整体接管用 apply-conf)")
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	dnsMode, _ := cmd.Flags().GetString("dns")
	noWire, _ := cmd.Flags().GetBool("no-wire")
	base, err := config.Load(p.Conf)
	if err != nil {
		return fmt.Errorf("读取基底配置失败: %w(先 panixy init/deploy 生成)", err)
	}
	personal, err := config.Load(args[0])
	if err != nil {
		return fmt.Errorf("读取个人配置失败: %w", err)
	}

	opts := config.MergeOpts{DNSMine: dnsMode == "mine", NoWire: noWire}
	rep, err := base.MergePersonal(personal, opts)
	if err != nil {
		return err
	}
	baseProviders := base.Providers()
	base.WireAfterMerge(baseProviders, rep.PersonalProxies, opts)

	// 决策报告(始终输出,dry-run 的主体)
	printMergeReport(rep, baseProviders)

	if dryRun {
		fmt.Fprintln(os.Stderr, "--dry-run:不落盘。融合结果预览输出到 stdout。")
		os.Stdout.WriteString(mustRender(base))
		return nil
	}

	// 落盘前先在临时文件上过内核 -t
	tmpConf := filepath.Join(os.TempDir(), fmt.Sprintf("panixy-merge-%d.yaml", time.Now().UnixNano()))
	defer os.Remove(tmpConf)
	os.WriteFile(tmpConf, []byte(mustRender(base)), 0o644)
	if out, err := mihomoTest(p, tmpConf); err != nil {
		return fmt.Errorf("融合结果未通过内核校验(%s),系统未做任何改动", firstErrLine(out))
	}

	logx.Step("备份基底 → 应用融合配置")
	if err := config.Backup(p.Conf); err != nil {
		return err
	}
	if err := os.WriteFile(p.Conf, []byte(mustRender(base)), 0o644); err != nil {
		config.Restore(p.Conf)
		return err
	}
	if err := systemdunit.Restart(); err != nil { // provider/分组结构变化,直接重启(热重载不刷新 provider)
		config.Restore(p.Conf)
		systemdunit.Restart()
		return fmt.Errorf("重启失败,已恢复原配置")
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		config.Restore(p.Conf)
		systemdunit.Restart()
		return fmt.Errorf("融合后健康检查超时,已恢复原配置:%w", err)
	}
	config.ClearBackup(p.Conf)
	logx.Info("融合完成:panixy sub-list 查看订阅;分组/节点选择在 Web 面板操作")
	return nil
}

func printMergeReport(r *config.MergeReport, baseProviders []string) {
	logx.Info("融合决策:")
	logx.Info("  接管(个人): %v", r.Taken)
	logx.Info("  保留(基底): %v", r.Kept)
	if len(r.Providers.BaseKept) > 0 {
		logx.Info("  订阅合并: 基底保留 %v", r.Providers.BaseKept)
	}
	if len(r.Providers.Personal) > 0 {
		logx.Info("  订阅合并: 个人新增 %v", r.Providers.Personal)
	}
	if len(r.Providers.Conflict) > 0 {
		logx.Warn("  订阅同名冲突(基底优先,含已预置缓存): %v", r.Providers.Conflict)
	}
	if len(r.RuleProvidersAdded) > 0 {
		logx.Info("  规则订阅并入: %v", r.RuleProvidersAdded)
	}
	if len(r.PersonalProxies) > 0 {
		logx.Info("  个人节点全部带入(%d 个,已追加进各组 proxies 末尾;select 默认不变,面板中自行挑选)",
			len(r.PersonalProxies))
	}
	for _, a := range r.Adjustments {
		logx.Info("  自动调整: %s", a)
	}
	_ = baseProviders
}

func mustRender(e *config.Editor) string {
	// 复用 Save 的归一化但不落盘:写到临时再读回
	tmp := filepath.Join(os.TempDir(), "panixy-render.yaml")
	old := e.Path()
	e.SetPath(tmp)
	defer e.SetPath(old)
	if err := e.Save(); err != nil {
		return fmt.Sprintf("# 渲染失败: %v\n", err)
	}
	b, _ := os.ReadFile(tmp)
	os.Remove(tmp)
	return string(b)
}
