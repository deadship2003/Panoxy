package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/health"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/mihomoapi"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/systemdunit"
	"github.com/deadship2003/Panoxy/internal/upgrade"
)

// runUpgrade is a parameterized upgrade: default core+ui; --core/--ui pick one; --check is a dry-run;
// --core-version/--ui-version pin a version. .last-upgrade is only updated when everything succeeds.
func runUpgrade(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error { return runUpgradeBody(p, cmd, args) })
}

func runUpgradeBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	uiOnly, _ := cmd.Flags().GetBool("ui")
	cliOnly, _ := cmd.Flags().GetBool("cli")
	check, _ := cmd.Flags().GetBool("check")
	uiVer, _ := cmd.Flags().GetString("ui-version")
	srcDir, _ := cmd.Flags().GetString("src")
	// Default upgrades the UI; --cli upgrades the Panoxy CLI itself (local self-compile). After fusion
	// the kernel is embedded in the CLI, so "upgrade the CLI" IS "upgrade the kernel" — there is no
	// separate prebuilt core to download anymore.
	anyOnly := uiOnly || cliOnly
	doUI := !anyOnly || uiOnly
	doCLI := cliOnly

	var err error
	api := mihomoapi.NewFromConf(p.Conf)
	proxy := api.Proxy()

	curUI := ""
	if b, err := os.ReadFile(p.UiStamp); err == nil {
		curUI = strings.TrimSpace(string(b))
	}

	var latestUI string
	if doUI {
		want := uiVer
		if want == "" {
			if latestUI, err = upgrade.Latest("MetaCubeX/metacubexd", proxy); err != nil {
				if !check {
					logx.Warn("UI version query failed, skipping this time: %v", err)
				}
			} else {
				want = latestUI
			}
		}
		if check {
			fmt.Printf("UI:    current %s latest %s → %s\n", orQ(curUI), orQ(want), action(curUI, want))
		} else if want != "" && want != curUI {
			if err := uiUpgrade(p, proxy, want); err != nil {
				return err
			}
		} else {
			logx.Info("UI is already latest %s", curUI)
		}
	}
	if doCLI {
		cur := version
		srcVer := cliSrcVersion(srcDir)
		if check {
			fmt.Printf("CLI:   current %s source %s → %s\n", orQ(cur), orQ(srcVer), action(cur, srcVer))
		} else if err := cliUpgrade(p, srcDir); err != nil {
			return err
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
		return "up to date"
	}
	if want == "" {
		return "query failed"
	}
	return "upgradable"
}

// uiUpgrade: download compressed-dist.tgz → swap dir → probe /ui/ → restore the old dir on failure.
func uiUpgrade(p paths.Paths, proxy, want string) error {
	logx.Info("UI upgrade: → %s", want)
	tmp, _ := os.MkdirTemp("", "panixy-ui-")
	defer os.RemoveAll(tmp)
	tgz := filepath.Join(tmp, "dist.tgz")
	url := "https://github.com/MetaCubeX/metacubexd/releases/download/" + want + "/compressed-dist.tgz"
	if err := upgrade.Download(url, proxy, tgz); err != nil {
		return fmt.Errorf("UI download failed: %w", err)
	}
	if err := runExtractTgz(tgz, filepath.Join(tmp, "x")); err != nil {
		return fmt.Errorf("UI unpack failed: %w", err)
	}
	if !exists(filepath.Join(tmp, "x", "index.html")) {
		return fmt.Errorf("UI package is abnormal (no index.html)")
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
		return fmt.Errorf("UI probe abnormal (http=%s), restored old version", code)
	}
	os.RemoveAll(p.UiDir + ".old")
	logx.Info("UI upgrade succeeded → %s", want)
	return nil
}

// runRollback rolls the Panoxy CLI back to a backup left by a --cli upgrade (default: the most recent).
func runRollback(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error { return runRollbackBody(p, cmd, args) })
}

func runRollbackBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	matches, _ := filepath.Glob(p.Cli + ".bak-*")
	if len(matches) == 0 {
		return fmt.Errorf("no backup available (run sudo %s upgrade --cli first to create one)", constants.ProgName)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	bak := matches[0]
	if len(args) > 0 {
		bak = p.Cli + ".bak-" + args[0]
		if !exists(bak) {
			return fmt.Errorf("backup does not exist: %s (existing: %s)", bak, strings.Join(matches, " "))
		}
	}
	// Back up the current CLI first (same naming as cliUpgrade), so rolling back can be repeated.
	cur := strings.TrimPrefix(version, "v")
	copyFile(p.Cli, p.Cli+".bak-"+cur)
	copyFile(bak, p.Cli)
	os.Chmod(p.Cli, 0o755)
	if err := systemdunit.Restart(); err != nil {
		return err
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		logx.Warn("health check failed after rollback: %v", err)
	}
	logx.Info("CLI rolled back %s → %s", cur, filepath.Base(bak))
	return nil
}

// cliUpgrade upgrades the Panoxy CLI itself: self-compile locally inside the source tree (go build) → replace the installed binary.
// It does not download prebuilt artifacts — a git clone contains only source (manual compile is quick); --src points to the repo root, defaulting to the current directory.
func cliUpgrade(p paths.Paths, srcDir string) error {
	logx.Info("CLI upgrade: local self-compile")
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("go toolchain not found, install Go 1.23+ first (or cd to the repo root and run go build manually)")
	}
	if srcDir == "" {
		srcDir, _ = os.Getwd()
	}
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err != nil {
		return fmt.Errorf("invalid source root (go.mod missing): %s; cd to the repo root or specify --src", srcDir)
	}
	newVer := cliSrcVersion(srcDir)
	if newVer == "" {
		newVer = version
	}
	tmp, _ := os.MkdirTemp("", constants.ProgName+"-cli-up-")
	defer os.RemoveAll(tmp)
	newBin := filepath.Join(tmp, constants.ProgName)
	ldflags := fmt.Sprintf("-s -w -X main.version=%s -X github.com/deadship2003/Panoxy/internal/constants.ProgName=%s -buildid=",
		newVer, constants.ProgName)
	cmd := exec.Command(goBin, "build", "-trimpath", "-ldflags", ldflags, "-o", newBin, "./cmd/panixy")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("self-compile failed: %v\n%s", err, out)
	}
	if out, err := exec.Command(newBin, "--version").CombinedOutput(); err != nil {
		return fmt.Errorf("new binary cannot run: %v\n%s", err, out)
	}

	// Back up the old version → replace.
	bak := p.Cli + ".bak-" + strings.TrimPrefix(version, "v")
	copyFile(p.Cli, bak)
	if err := copyFile(newBin, p.Cli); err != nil {
		copyFile(bak, p.Cli) // rollback
		return fmt.Errorf("CLI replace failed: %w", err)
	}
	os.Chmod(p.Cli, 0o755)
	logx.Info("CLI self-compile succeeded → %s (backup %s)", newVer, bak)
	return nil
}

// cliSrcVersion returns the source tree's git describe (same source as build.sh); returns empty for a non-git directory.
func cliSrcVersion(srcDir string) string {
	if srcDir == "" {
		srcDir, _ = os.Getwd()
	}
	out, err := exec.Command("git", "-C", srcDir, "describe", "--tags").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
