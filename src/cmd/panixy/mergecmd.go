package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/config"
	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/health"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/systemdunit"
)

// runMergeConf 叠加式融合:同名组合并 + 新增追加 + 基底保留 + 备份回滚。
func runMergeConf(cmd *cobra.Command, args []string) error {
	// --rollback:从 premerge 备份恢复
	if rb, _ := cmd.Flags().GetBool("rollback"); rb {
		return mergeRollback()
	}
	return withRootLock(func(p paths.Paths) error { return runMergeConfBody(p, cmd, args) })
}

func runMergeConfBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("用法: %s merge-conf <个人配置.yaml>(叠加融合;--dry-run 试运行;--rollback 回滚)", constants.ProgName)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	dnsMode, _ := cmd.Flags().GetString("dns")
	noWire, _ := cmd.Flags().GetBool("no-wire")

	// 基底用纯净默认模板(config.default.yaml),而非运行中的 /etc/clash.yaml ——
	// 保证融合结果可复现:个人配置叠加到干净基线上,占位订阅(SUB_URL_PLACEHOLDER)由 MergePersonal 自动退场。
	base, err := config.Load(p.DefaultConf)
	if err != nil {
		return fmt.Errorf("读取默认基底配置失败: %w(%s 不存在,先 sudo %s redeploy 生成)", err, p.DefaultConf, constants.ProgName)
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

	printMergeReport(rep, baseProviders)

	if dryRun {
		fmt.Fprintln(os.Stderr, "--dry-run:试运行模式,不落盘不备份。融合结果预览输出到 stdout。")
		os.Stdout.WriteString(mustRender(base))
		return nil
	}

	// 备份(premerge)→ 融合 → -t → 应用 → 健康 → 失败恢复
	logx.Step("备份当前配置 → %s", p.Conf+constants.PremergeSuffix())
	bakPath, err := config.PremergeBackup(p.Conf)
	if err != nil {
		return fmt.Errorf("premerge 备份失败: %w", err)
	}
	rep.BackupPath = bakPath

	tmpConf := filepath.Join(os.TempDir(), fmt.Sprintf("panixy-merge-%d.yaml", time.Now().UnixNano()))
	defer os.Remove(tmpConf)
	os.WriteFile(tmpConf, []byte(mustRender(base)), 0o644)
	if out, err := mihomoTest(p, tmpConf); err != nil {
		return fmt.Errorf("融合结果未通过内核校验(%s),系统未做任何改动;可用 --dry-run 检查", firstErrLine(out))
	}

	logx.Step("应用融合配置 → 重启服务")
	if err := os.WriteFile(p.Conf, []byte(mustRender(base)), 0o644); err != nil {
		config.PremergeRestore(p.Conf)
		return err
	}
	if err := systemdunit.Restart(); err != nil {
		config.PremergeRestore(p.Conf)
		systemdunit.Restart()
		return fmt.Errorf("重启失败,已从 premerge 恢复")
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		config.PremergeRestore(p.Conf)
		systemdunit.Restart()
		return fmt.Errorf("融合后健康检查超时,已从 premerge 恢复:%w", err)
	}
	logx.Info("融合完成:%s sub list 查看订阅;分组/节点选择在 Web 面板操作", constants.ProgName)
	logx.Info("回滚: sudo %s merge-conf --rollback(恢复到融合前)", constants.ProgName)
	return nil
}

func mergeRollback() error {
	return withRootLock(func(p paths.Paths) error {
		if !config.PremergeExists(p.Conf) {
			return fmt.Errorf("无 premerge 备份(%s%s 不存在)", p.Conf, constants.PremergeSuffix())
		}
		if err := config.PremergeRestore(p.Conf); err != nil {
			return fmt.Errorf("恢复失败: %w", err)
		}
		if err := systemdunit.Restart(); err != nil {
			return fmt.Errorf("配置已恢复但重启失败: %w", err)
		}
		logx.Info("已从 premerge 备份恢复并重启")
		return nil
	})
}

func printMergeReport(r *config.MergeReport, _ []string) {
	logx.Info("融合决策:")
	if len(r.GroupsMerged) > 0 {
		logx.Info("  组融合(同名): %v", r.GroupsMerged)
	}
	if len(r.GroupsAdded) > 0 {
		logx.Info("  组新增(个人): %v", r.GroupsAdded)
	}
	if len(r.GroupsKept) > 0 {
		logx.Info("  组保留(基底): %v(共 %d 组)", r.GroupsKept[:min(5, len(r.GroupsKept))], len(r.GroupsKept))
		if len(r.GroupsKept) > 5 {
			logx.Info("    ...等")
		}
	}
	if r.RulesPersonal > 0 || r.RulesBase > 0 {
		logx.Info("  规则: 个人 %d 条前置 + 基底 %d 条兜底(去重 %d)", r.RulesPersonal, r.RulesBase, r.RulesDeduped)
	}
	logx.Info("  接管(个人): %v", r.Taken)
	logx.Info("  保留(基底): %v", r.Kept)
	if len(r.Providers.BaseKept) > 0 {
		logx.Info("  订阅基底: %v", r.Providers.BaseKept)
	}
	if len(r.Providers.Personal) > 0 {
		logx.Info("  订阅新增: %v", r.Providers.Personal)
	}
	if len(r.Providers.Conflict) > 0 {
		logx.Warn("  订阅同名(基底优先): %v", r.Providers.Conflict)
	}
	if len(r.RuleProvidersAdded) > 0 {
		logx.Info("  规则订阅并入: %v", r.RuleProvidersAdded)
	}
	if len(r.PersonalProxies) > 0 {
		logx.Info("  个人节点带入(%d 个,追加进组末尾)", len(r.PersonalProxies))
	}
	for _, a := range r.Adjustments {
		logx.Info("  自动调整: %s", a)
	}
	if r.BackupPath != "" {
		logx.Info("  备份: %s(--rollback 可恢复)", r.BackupPath)
	}
}

func mustRender(e *config.Editor) string {
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
