package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/asset"
	"github.com/deadship2003/panixy/internal/config"
	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/locker"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/statemode"
	"github.com/deadship2003/panixy/internal/systemdunit"
)

func healthReadMode(statePath string) string {
	return statemode.Read(statePath)
}

// runInstall 仅部署服务与系统设置(文件已就位;deploy 的内部步骤)。
func runInstall(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()

	mode := statemode.Read(p.State)
	logx.Step("[1/4] 预检:内核可执行 + 配置过 -t")
	if err := checkBinary(p); err != nil {
		return err
	}
	if out, err := mihomoTest(p, p.Conf); err != nil {
		return fmt.Errorf("配置校验未通过(%s)", firstErrLine(out))
	}

	logx.Step("[2/4] 写入 systemd 单元(模式 %s)并开启 ip_forward", mode)
	prevFwd := readIPForward()
	if err := systemdunit.Write(p, mode); err != nil {
		return err
	}
	writeSysctl(p)
	rollback := func() {
		systemdunit.Stop()
		systemdunit.Remove(p)
		os.Remove(p.Sysctl)
		setIPForward(prevFwd)
		logx.Warn("已回滚:unit/timer/sysctl 移除,ip_forward 恢复为 %s", prevFwd)
	}

	logx.Step("[3/4] 拉起服务(ExecStartPost 自动加载防火墙)")
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

	logx.Step("[4/4] 健康验证(服务+API)")
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		rollback()
		return fmt.Errorf("健康验证超时,已回滚")
	}
	logx.Info("install 完成 v%s(健康验证通过)", constants.Version)
	return nil
}

// runDeploy 全新部署(离线包内运行):资产→配置→CLI/手册→服务→可选订阅导入。
func runDeploy(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()

	pkgDir, err := os.Getwd() // deploy 须在解压的离线包根目录运行
	if err != nil {
		return err
	}
	assets := filepath.Join(pkgDir, "assets")
	if _, err := os.Stat(assets); err != nil {
		return fmt.Errorf("当前目录无离线资产(%s)—— deploy 需在解压的 Panixy 离线包内运行", assets)
	}
	// bash 旧版残留检测:中止并给手动清理指引(Q7 决议:不做自动迁移)
	if legacy := systemdunit.DetectLegacy(p); legacy != "" {
		return fmt.Errorf(`检测到 bash 旧版部署残留:%s
请先手动清理后重试:
  sudo panixy uninstall && sudo systemctl revert ...
  详见 README「从 bash 版迁移」一节`, legacy)
	}

	mode, _ := cmd.Flags().GetString("proxy-mode")
	if mode != "tun" && mode != "tproxy" {
		return fmt.Errorf("--proxy-mode 只能是 tun 或 tproxy")
	}

	snap := snapshot(p)
	defer func() { /* 失败路径各自显式回滚 */ _ = snap }()

	logx.Step("[1/6] 内核:解包 assets 内 %s 版本", goArch())
	if err := placeCore(p, assets); err != nil {
		return err
	}
	logx.Step("[2/6] geo 与广告规则就位(离线预置)")
	placeGeoAndRules(p, assets)
	logx.Step("[3/6] Web UI 就位")
	placeUI(p, assets)

	logx.Step("[4/6] 配置:现有 > 包内手工 clash.yaml > 模板渲染")
	confNew := false
	if _, err := os.Stat(p.Conf); err == nil {
		logx.Info("检测到现有配置,保留不动: %s", p.Conf)
	} else if b, err := os.ReadFile(filepath.Join(pkgDir, "clash.yaml")); err == nil {
		if err := os.WriteFile(p.Conf, b, 0o644); err != nil {
			return err
		}
		logx.Info("采用包内手工配置: clash.yaml")
	} else {
		d := asset.DefaultConfigData()
		d.TProxy = mode == "tproxy"
		d.Secret = randHex(16)
		out, err := asset.RenderConfig(d)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p.Conf, []byte(out), 0o644); err != nil {
			return err
		}
		confNew = true
		logx.Info("写入基础配置(模式 %s);面板密钥: %s(查看: grep '^secret' %s)", mode, d.Secret, p.Conf)
	}
	_ = confNew

	logx.Step("[5/6] CLI 与 man 手册就位;状态文件写入 proxy-mode=%s", mode)
	self, err := os.Executable()
	if err != nil {
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
	os.MkdirAll(filepath.Dir(p.State), 0o755)
	statemode.Write(p.State, statemode.State{ProxyMode: mode})

	logx.Step("[6/6] 服务部署(含防火墙;失败全量回滚)")
	if err := runInstall(cmd, args); err != nil {
		deployRollback(p, snap)
		return err
	}

	if len(args) > 0 {
		return runSetSub(cmd, args)
	}
	logx.Info("deploy 完成。提示: sudo panixy set-sub 设置订阅(回车进入粘贴模式);panixy status 查看健康")
	return nil
}

// runUninstall 停服务、清防火墙与单元;保留 /opt 数据与配置。
func runUninstall(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	systemdunit.Stop()
	if fw, err := firewallNew(); err == nil {
		if err := fw.Teardown(); err != nil {
			logx.Warn("防火墙清理失败:%v(restart 后重试 uninstall)", err)
		}
	}
	systemdunit.Remove(p)
	os.Remove(p.Sysctl)
	os.Remove(p.ManGz)
	logx.Info("已卸载 unit/timer/sysctl/手册;数据目录 %s 与 %s 保留(CLI 本身保留)", p.Root, p.Conf)
	return nil
}

// runModeSwitch 原子切换:旧防火墙卸载 → 配置变体 → -t → 重启 → 新防火墙 → 验证。
func modeSwitch(want string) error {
	if err := needRoot(); err != nil {
		return err
	}
	if want != "tun" && want != "tproxy" {
		return fmt.Errorf("模式只能是 tun 或 tproxy")
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	old := statemode.Read(p.State)
	if old == want {
		logx.Info("当前已是 %s 模式", want)
		return nil
	}
	if want == "tproxy" && os.Getenv("PANIXY_SKIP_TPROXY_PROBE") == "" {
		if err := checkTproxySupport(); err != nil {
			return fmt.Errorf("TPROXY 前置条件不满足:%v", err)
		}
	} // PANIXY_SKIP_TPROXY_PROBE=1 仅测试沙箱用
	logx.Step("切换 %s → %s:卸载旧防火墙", old, want)
	if fw, err := firewallNew(); err == nil {
		fw.Teardown()
	}
	logx.Step("渲染配置变体并校验")
	if err := config.Backup(p.Conf); err != nil {
		return err
	}
	e, err := config.Load(p.Conf)
	if err != nil {
		return err
	}
	e.SetMode(want == "tproxy", constants.TproxyPort)
	if err := e.Save(); err != nil {
		config.Restore(p.Conf)
		return err
	}
	if out, err := mihomoTest(p, p.Conf); err != nil {
		msg := firstErrLine(out)
		config.Restore(p.Conf)
		return fmt.Errorf("配置校验未通过(%s),已恢复", msg)
	}
	statemode.Write(p.State, statemode.State{ProxyMode: want})
	if err := systemdunit.Restart(); err != nil {
		config.Restore(p.Conf)
		statemode.Write(p.State, statemode.State{ProxyMode: old})
		systemdunit.Restart()
		return fmt.Errorf("重启失败,已回滚到 %s", old)
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		config.Restore(p.Conf)
		statemode.Write(p.State, statemode.State{ProxyMode: old})
		systemdunit.Restart()
		return fmt.Errorf("切换后健康检查超时,已回滚到 %s:%w", old, err)
	}
	config.ClearBackup(p.Conf)
	logx.Info("已切换到 %s 模式(数据面选择仍在 Web 面板)", want)
	return nil
}

// ---- deploy/install 辅助 ----

type deploySnap struct {
	rootNew, confNew, cliNew, manNew, stateNew bool
	prevFwd                                    string
}

func snapshot(p paths.Paths) deploySnap {
	return deploySnap{
		rootNew:  !exists(p.Root),
		confNew:  !exists(p.Conf),
		cliNew:   !exists(p.Cli),
		manNew:   !exists(p.ManGz),
		stateNew: !exists(p.State),
		prevFwd:  readIPForward(),
	}
}

func deployRollback(p paths.Paths, s deploySnap) {
	systemdunit.Stop()
	systemdunit.Remove(p)
	os.Remove(p.Sysctl)
	setIPForward(s.prevFwd)
	if s.confNew {
		os.Remove(p.Conf)
	}
	if s.cliNew {
		os.Remove(p.Cli)
	}
	if s.manNew {
		os.Remove(p.ManGz)
	}
	if s.stateNew {
		os.Remove(p.State)
	}
	if s.rootNew {
		os.RemoveAll(p.Root)
		logx.Warn("回滚:新建数据目录已删除")
	} else {
		logx.Warn("回滚:%s 原本已存在,本次新增文件保留在原地", p.Root)
	}
	logx.Warn("回滚完成,系统已恢复原状")
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func checkBinary(p paths.Paths) error {
	if !exists(p.Bin) {
		return fmt.Errorf("内核不存在: %s(在离线包内用 panixy deploy,或手动放置)", p.Bin)
	}
	// 教训:空/损坏内核经 ENOEXEC 会被当空脚本执行,-v 假通过 —— 必须校验输出内容
	if out := runCmd(p.Bin, "-v"); !strings.Contains(out, "Mihomo") {
		return fmt.Errorf("内核无法运行(空文件/架构不符?): %s", p.Bin)
	}
	return nil
}

func readIPForward() string {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return "0"
	}
	return strings.TrimSpace(string(b))
}

func writeSysctl(p paths.Paths) {
	os.MkdirAll(filepath.Dir(p.Sysctl), 0o755)
	os.WriteFile(p.Sysctl, []byte("net.ipv4.ip_forward = 1\n"), 0o644)
	setIPForward("1")
}

func setIPForward(v string) {
	runCmd("sysctl", "-w", "net.ipv4.ip_forward="+v)
}

func goArch() string {
	a := map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtimeArch()]
	return a
}

func placeCore(p paths.Paths, assets string) error {
	if exists(p.Bin) {
		logx.Info("内核已存在,保留")
		return nil
	}
	arch := runtimeArch()
	if arch == "" {
		return fmt.Errorf("不支持的架构(包内置 amd64/arm64)")
	}
	matches, _ := filepath.Glob(filepath.Join(assets, "core", "mihomo-linux-"+arch+"-*.gz"))
	if len(matches) == 0 {
		return fmt.Errorf("assets 缺 %s 内核", arch)
	}
	core := matches[len(matches)-1]
	if err := gunzipTo(core, p.Bin); err != nil {
		return err
	}
	os.Chmod(p.Bin, 0o755)
	if err := checkBinary(p); err != nil {
		return err
	}
	logx.Info("内核: %s", firstLineOf(runCmd(p.Bin, "-v")))
	return nil
}

func placeGeoAndRules(p paths.Paths, assets string) {
	for _, f := range []string{"GeoIP.dat", "GeoSite.dat", "Country.mmdb"} {
		src := filepath.Join(assets, "geo", f)
		dst := filepath.Join(p.Root, f)
		if !exists(dst) && exists(src) {
			copyFile(src, dst)
		}
	}
	os.MkdirAll(p.RuleProv, 0o755)
	src := filepath.Join(assets, "rule", "AWAvenue-Ads.yaml")
	dst := filepath.Join(p.RuleProv, "AWAvenue-Ads.yaml")
	if !exists(dst) {
		if exists(src) {
			copyFile(src, dst)
		} else {
			logx.Warn("包内未带广告规则文件,首启由内核联网拉取")
		}
	}
}

func placeUI(p paths.Paths, assets string) {
	if exists(p.UiDir) {
		return
	}
	src := filepath.Join(assets, "ui", "official")
	if exists(src) {
		copyDir(src, p.UiDir)
		os.WriteFile(p.UiStamp, []byte("unknown\n"), 0o644)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	randRead(b)
	return fmt.Sprintf("%x", b)
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return strings.TrimSpace(s)
}
