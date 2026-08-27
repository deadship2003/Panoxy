package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/locker"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/systemdunit"
	"github.com/deadship2003/panixy/internal/upgrade"
)

// runUpgrade 参数化升级:默认 core+ui;--core/--ui 二选一;--check dry-run;
// --core-version/--ui-version 指定版本。全成功才更新 .last-upgrade。
func runUpgrade(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()

	coreOnly, _ := cmd.Flags().GetBool("core")
	uiOnly, _ := cmd.Flags().GetBool("ui")
	check, _ := cmd.Flags().GetBool("check")
	coreVer, _ := cmd.Flags().GetString("core-version")
	uiVer, _ := cmd.Flags().GetString("ui-version")
	doCore := !uiOnly || coreOnly
	doUI := !coreOnly || uiOnly

	api := mihomoapi.NewFromConf(p.Conf)
	proxy := ""
	if api.Mixed > 0 {
		proxy = fmt.Sprintf("http://127.0.0.1:%d", api.Mixed)
	}

	curCore := ""
	if out := runCmd(p.Bin, "-v"); out != "" {
		curCore = firstVer(out)
	}
	curUI := ""
	if b, err := os.ReadFile(p.UiStamp); err == nil {
		curUI = strings.TrimSpace(string(b))
	}

	var latestCore, latestUI string
	if doCore {
		want := coreVer
		if want == "" {
			if latestCore, err = upgrade.Latest("MetaCubeX/mihomo", proxy); err != nil {
				if !check {
					logx.Warn("内核版本查询失败,本次跳过:%v", err)
				}
			} else {
				want = latestCore
			}
		}
		if check {
			fmt.Printf("内核: 当前 %s 最新 %s → %s\n", orQ(curCore), orQ(want), action(curCore, want))
		} else if want != "" && want != curCore {
			if err := coreUpgrade(p, proxy, want); err != nil {
				return err
			}
		} else {
			logx.Info("内核已是最新 %s", curCore)
		}
	}
	if doUI {
		want := uiVer
		if want == "" {
			if latestUI, err = upgrade.Latest("MetaCubeX/metacubexd", proxy); err != nil {
				if !check {
					logx.Warn("UI 版本查询失败,本次跳过:%v", err)
				}
			} else {
				want = latestUI
			}
		}
		if check {
			fmt.Printf("UI:   当前 %s 最新 %s → %s\n", orQ(curUI), orQ(want), action(curUI, want))
		} else if want != "" && want != curUI {
			if err := uiUpgrade(p, proxy, want); err != nil {
				return err
			}
		} else {
			logx.Info("UI 已是最新 %s", curUI)
		}
	}
	if check {
		return nil
	}
	os.WriteFile(p.LastUp, []byte(time.Now().Format("2006-01-02 15:04:05")+"\n"), 0o644)
	return nil
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func action(cur, want string) string {
	if cur == want {
		return "无需升级"
	}
	if want == "" {
		return "查询失败"
	}
	return "可升级"
}

// coreUpgrade:下载候选资产→试运行→备份→替换→重启→双健康检查→失败回滚。
func coreUpgrade(p paths.Paths, proxy, want string) error {
	logx.Info("内核升级: → %s", want)
	tmp, _ := os.MkdirTemp("", "panixy-up-")
	defer os.RemoveAll(tmp)
	var got string
	for _, base := range upgrade.CoreAssetCandidates(want) {
		gz := filepath.Join(tmp, "core.gz")
		url := "https://github.com/MetaCubeX/mihomo/releases/download/" + want + "/" + base + ".gz"
		logx.Step("尝试资产 %s", base)
		if err := upgrade.Download(url, proxy, gz); err != nil {
			logx.Step("下载失败(404/网络),降级下一档")
			continue
		}
		core := filepath.Join(tmp, "core")
		if err := upgrade.GunzipFile(gz, core); err != nil {
			continue
		}
		os.Chmod(core, 0o755)
		if err := upgrade.VerifyCore(core, want); err != nil {
			logx.Step("%v,降级下一档", err)
			continue
		}
		got = core
		break
	}
	if got == "" {
		return fmt.Errorf("所有候选资产均失败")
	}
	cur := firstVer(runCmd(p.Bin, "-v"))
	bak := p.Bin + ".bak-" + cur
	copyFile(p.Bin, bak)
	if err := copyFile(got, p.Bin+".new"); err != nil {
		return err
	}
	os.Rename(p.Bin+".new", p.Bin)
	os.Chmod(p.Bin, 0o755)
	if err := systemdunit.Restart(); err != nil {
		restoreCore(p, bak, cur)
		return fmt.Errorf("重启失败,已回滚到 %s", cur)
	}
	if err := health.WaitHealthy(p.Conf, 90*time.Second, want); err != nil || !health.EgressOK(mihomoapi.NewFromConf(p.Conf).Mixed, 3) {
		restoreCore(p, bak, cur)
		return fmt.Errorf("升级健康检查失败,已回滚到 %s", cur)
	}
	logx.Info("内核升级成功 → %s(备份 %s)", want, bak)
	pruneCoreBackups(p, constants.CoreKeep)
	return nil
}

func restoreCore(p paths.Paths, bak, cur string) {
	copyFile(bak, p.Bin)
	os.Chmod(p.Bin, 0o755)
	systemdunit.Restart()
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		logx.Warn("回滚后健康检查仍未通过,请 panixy log 排查")
	}
}

func pruneCoreBackups(p paths.Paths, keep int) {
	matches, _ := filepath.Glob(p.Bin + ".bak-*")
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, m := range matches[keep:] {
		os.Remove(m)
	}
}

// uiUpgrade:下载 compressed-dist.tgz → 换目录 → /ui/ 探活 → 失败恢复旧目录。
func uiUpgrade(p paths.Paths, proxy, want string) error {
	logx.Info("UI 升级: → %s", want)
	tmp, _ := os.MkdirTemp("", "panixy-ui-")
	defer os.RemoveAll(tmp)
	tgz := filepath.Join(tmp, "dist.tgz")
	url := "https://github.com/MetaCubeX/metacubexd/releases/download/" + want + "/compressed-dist.tgz"
	if err := upgrade.Download(url, proxy, tgz); err != nil {
		return fmt.Errorf("UI 下载失败: %w", err)
	}
	if err := runExtractTgz(tgz, filepath.Join(tmp, "x")); err != nil {
		return fmt.Errorf("UI 解包失败: %w", err)
	}
	if !exists(filepath.Join(tmp, "x", "index.html")) {
		return fmt.Errorf("UI 包异常(无 index.html)")
	}
	os.RemoveAll(p.UiDir + ".old")
	if exists(p.UiDir) {
		os.Rename(p.UiDir, p.UiDir+".old")
	}
	copyDir(filepath.Join(tmp, "x"), p.UiDir)
	os.WriteFile(p.UiStamp, []byte(want+"\n"), 0o644)
	if code := probeUI(p.Conf); code != "200" {
		os.RemoveAll(p.UiDir)
		os.Rename(p.UiDir+".old", p.UiDir)
		os.WriteFile(p.UiStamp, []byte("unknown\n"), 0o644)
		return fmt.Errorf("UI 探活异常(http=%s),已恢复旧版", code)
	}
	os.RemoveAll(p.UiDir + ".old")
	logx.Info("UI 升级成功 → %s", want)
	return nil
}

// runRollback 回滚内核二进制(默认最近备份)。
func runRollback(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	matches, _ := filepath.Glob(p.Bin + ".bak-*")
	if len(matches) == 0 {
		return fmt.Errorf("没有可用备份")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	bak := matches[0]
	if len(args) > 0 {
		bak = p.Bin + ".bak-" + args[0]
		if !exists(bak) {
			return fmt.Errorf("备份不存在: %s(现有: %s)", bak, strings.Join(matches, " "))
		}
	}
	cur := firstVer(runCmd(p.Bin, "-v"))
	copyFile(p.Bin, p.Bin+".bak-"+cur)
	copyFile(bak, p.Bin)
	os.Chmod(p.Bin, 0o755)
	if err := systemdunit.Restart(); err != nil {
		return err
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		logx.Warn("回滚后健康检查未通过: %v", err)
	}
	logx.Info("内核回滚 %s → %s", cur, filepath.Base(bak))
	return nil
}

func firstVer(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if v := upgradeVerRe(s); v != "" {
		return v
	}
	return s
}

// runUpdateUI 等价 upgrade --ui。
func runUpdateUI(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	api := mihomoapi.NewFromConf(p.Conf)
	proxy := ""
	if api.Mixed > 0 {
		proxy = fmt.Sprintf("http://127.0.0.1:%d", api.Mixed)
	}
	want, err := upgrade.Latest("MetaCubeX/metacubexd", proxy)
	if err != nil {
		return fmt.Errorf("UI 版本查询失败: %v", err)
	}
	return uiUpgrade(p, proxy, want)
}
