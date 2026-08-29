package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/firewall"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/statemode"
	"github.com/deadship2003/panixy/internal/systemdunit"
)

// applyFW 按当前模式显式加载防火墙规则(fw apply 的复用)。
// redeploy 用它显式重挂 FW —— 新编译的 panixy 可能调整了规则,不能只靠服务重启的
// ExecStartPost 兜底(那是隐式副作用,这里要求一等公民)。
func applyFW(mode string) error {
	fw, err := firewall.New()
	if err != nil {
		return err
	}
	if mode == "tproxy" {
		return fw.ApplyTproxy()
	}
	return fw.ApplyDnsHijack()
}

func cmdRedeploy() *cobra.Command {
	c := &cobra.Command{
		Use:   "redeploy",
		Short: "就地重装:从离线包强制刷新全部程序文件(保留配置),重挂防火墙并重启",
		Long: `在已安装机器上,从解压的离线包根目录强制刷新全部程序文件并重新部署。

与 deploy 的区别:deploy 对已存在的内核/geo/规则/UI 一律"存在即跳过";redeploy 则
全部强制覆盖 —— 用于把新编译的版本推到已安装机器,无需联网、无需卸载重装。

流程:停服务+清防火墙 → 备份内核/UI(供回滚)→ 强制替换内核/geo/规则/UI →
刷新 CLI/手册/systemd 单元 → 校验+重启 → 显式重挂防火墙 → 健康验证。
任一步失败回滚内核/UI(CLI 属管理工具不回滚;config.yaml 始终保留不动)。
切换透明代理模式请用 panixy mode(模式属数据,redeploy 不碰)。

示例:
  sudo ./panixy redeploy                # 在解压的新版离线包根目录运行
  sudo ./panixy redeploy --dry-run      # 试运行:预览将替换的文件与决策`,
		RunE: runRedeploy,
	}
	addDryRunFlag(c, "试运行模式:预览落位与决策,不执行")
	return c
}

func runRedeploy(cmd *cobra.Command, args []string) error {
	if dry, _ := cmd.Flags().GetBool("dry-run"); dry {
		return redeployDryRun(cmd)
	}
	return withRootLock(func(p paths.Paths) error { return runRedeployBody(p, cmd, args) })
}

func runRedeployBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	// 预检:必须已安装(全新装用 deploy);离线包资产齐全;无 bash 旧版残留
	if !exists(p.Bin) || !exists(p.Conf) || !exists(p.Cli) {
		return fmt.Errorf("未检测到已安装的 panixy(内核/配置/CLI 缺失)—— 全新安装请用 sudo ./panixy deploy")
	}
	pkgDir, err := os.Getwd() // redeploy 须在解压的离线包根目录运行
	if err != nil {
		return err
	}
	assets := filepath.Join(pkgDir, "assets")
	if _, err := os.Stat(assets); err != nil {
		return fmt.Errorf("当前目录无离线资产(%s)—— redeploy 需在解压的 Panixy 离线包内运行", assets)
	}
	if legacy := systemdunit.DetectLegacy(p); legacy != "" {
		return fmt.Errorf("检测到 bash 旧版部署残留:%s\n请先手动清理(详见 README「从 bash 版迁移」)", legacy)
	}
	mode := statemode.Read(p.State)
	if mode == "" {
		mode = "tun"
	}
	logx.Info("redeploy 开始:就地刷新程序文件(模式 %s,config.yaml 保留不动)", mode)

	// [1] 停服务并显式清防火墙(新二进制可能改了 FW 规则,不能只靠重启的 ExecStartPost)
	logx.Step("[1/6] 停止服务并清除防火墙规则")
	systemdunit.Stop()
	if fw, err := firewall.New(); err == nil {
		if err := fw.Teardown(); err != nil {
			logx.Warn("防火墙清理失败:%v(重启后 fw apply 会兜底)", err)
		}
	}

	// [2] 备份内核/UI(失败回滚数据面;geo/规则为静态数据无需回滚)
	logx.Step("[2/6] 备份当前内核与 UI")
	cur := firstVer(runCmd(p.Bin, "-v"))
	bak := p.Bin + ".bak-" + cur
	if err := copyFile(p.Bin, bak); err != nil {
		return err
	}
	if exists(p.UiDir) {
		os.RemoveAll(p.UiDir + ".old")
		if err := os.Rename(p.UiDir, p.UiDir+".old"); err != nil {
			return err
		}
	}
	rollback := func() {
		copyFile(bak, p.Bin)
		os.Chmod(p.Bin, 0o755)
		if exists(p.UiDir + ".old") {
			os.RemoveAll(p.UiDir)
			os.Rename(p.UiDir+".old", p.UiDir)
		}
		systemdunit.Write(p, mode)
		systemdunit.EnableNow()
		applyFW(mode)
		logx.Warn("redeploy 失败,已回滚内核/UI 并重启旧服务(CLI 保持新版,config 未动)")
	}

	// [3] 强制替换程序文件
	logx.Step("[3/6] 强制替换内核/geo/规则/UI")
	if err := placeCoreForce(p, assets); err != nil {
		rollback()
		return err
	}
	placeGeoAndRulesForce(p, assets)
	if err := placeUIForce(p, assets); err != nil {
		rollback()
		return err
	}

	// [4] 刷新 CLI/手册/systemd 单元/sysctl
	logx.Step("[4/6] 刷新 CLI/手册/systemd 单元/sysctl")
	self, err := os.Executable()
	if err != nil {
		rollback()
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if self != p.Cli {
		os.MkdirAll(filepath.Dir(p.Cli), 0o755)
		if b, err := os.ReadFile(self); err == nil {
			os.WriteFile(p.Cli, b, 0o755)
		}
	}
	installMan(p.ManGz, self)
	if err := systemdunit.Write(p, mode); err != nil {
		rollback()
		return err
	}
	writeSysctl(p)
	// 刷新纯净默认模板副本(与当前模板/模式/密钥同步,不含订阅),供 merge-conf 重建基线
	if err := writeDefaultConf(p, mode, mihomoapi.NewFromConf(p.Conf).Secret); err != nil {
		logx.Warn("刷新默认配置副本失败:%v", err)
	}

	// [5] 校验 + 拉起服务
	logx.Step("[5/6] 校验配置并重启服务")
	if out, err := mihomoTest(p, p.Conf); err != nil {
		rollback()
		return fmt.Errorf("配置校验未通过(%s),已回滚", firstErrLine(out))
	}
	if err := systemdunit.PortCheck(p.Conf); err != nil {
		rollback()
		return err
	}
	if err := systemdunit.EnableNow(); err != nil {
		rollback()
		return fmt.Errorf("服务启动失败,已回滚")
	}
	if err := systemdunit.EnableTimer(); err != nil {
		rollback()
		return fmt.Errorf("升级 timer 启用失败,已回滚")
	}

	// [6] 显式重挂防火墙(新规则)+ 健康验证(与 ExecStartPost 重复但保证一等公民)
	logx.Step("[6/6] 重新部署防火墙并健康验证")
	if err := applyFW(mode); err != nil {
		rollback()
		return fmt.Errorf("防火墙部署失败,已回滚:%v", err)
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		rollback()
		return fmt.Errorf("健康验证超时,已回滚")
	}

	// 成功收尾:清 UI 备份、prune 内核备份、更新升级时间戳
	os.RemoveAll(p.UiDir + ".old")
	pruneCoreBackups(p, constants.CoreKeep)
	os.WriteFile(p.LastUp, []byte(time.Now().Format("2006-01-02 15:04:05")+"\n"), 0o644)
	logx.Info("redeploy 完成 v%s:内核/geo/规则/UI/CLI 已刷新,防火墙已重挂,配置保留", constants.Version)
	return nil
}

// redeployDryRun 试运行:核对资产、决策与将替换的文件清单。
func redeployDryRun(cmd *cobra.Command) error {
	p := paths.Get()
	logx.Info("== redeploy --dry-run(试运行,不执行)==")
	if !exists(p.Bin) || !exists(p.Conf) || !exists(p.Cli) {
		logx.Warn("未检测到已安装的 panixy;全新安装请用 sudo ./panixy deploy")
	}
	pkgDir, _ := os.Getwd()
	assets := filepath.Join(pkgDir, "assets")
	logx.Step("[预检] 离线包资产(%s)", assets)
	for _, item := range []struct{ name, path string }{
		{"内核(" + runtimeArch() + ")", filepath.Join(assets, "core")},
		{"GeoIP.dat", filepath.Join(assets, "geo", "GeoIP.dat")},
		{"GeoSite.dat", filepath.Join(assets, "geo", "GeoSite.dat")},
		{"Country.mmdb", filepath.Join(assets, "geo", "Country.mmdb")},
		{"广告规则", filepath.Join(assets, "rule", "AWAvenue-Ads.yaml")},
		{"面板", filepath.Join(assets, "ui", "official", "index.html")},
	} {
		if exists(item.path) {
			logx.Info("  ✓ %s", item.name)
		} else {
			logx.Warn("  ✗ %s 缺失", item.name)
		}
	}
	mode := statemode.Read(p.State)
	if mode == "" {
		mode = "tun"
	}
	logx.Step("[计划] 强制替换:内核/geo/规则/UI/CLI/手册/单元 → 清FW → 重启 → 重挂FW(模式 %s)", mode)
	logx.Info("保留不动: %s(订阅/节点选择/分组)与 %s", p.Conf, p.Proxies)
	logx.Info("== 试运行结束。真装: sudo ./panixy redeploy")
	return nil
}
