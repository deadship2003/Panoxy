package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/firewall"
	"github.com/deadship2003/Panoxy/internal/health"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/mihomoapi"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/statemode"
	"github.com/deadship2003/Panoxy/internal/systemdunit"
)

// applyFW explicitly loads firewall rules for the current mode (shared with fw apply).
// redeploy uses it to explicitly re-mount the firewall — a newly compiled panixy may have
// adjusted rules, so it can't rely solely on the service restart's ExecStartPost fallback
// (that is an implicit side effect; here it is a first-class citizen).
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
		Short: "in-place redeploy: force-refresh all program files from an offline package (config kept), re-mount firewall and restart",
		Long: `On an installed machine, force-refresh all program files from the root of an extracted offline
package and redeploy.

Difference from deploy: deploy skips any kernel/geo/rules/UI that already exist ("exists = skip");
redeploy force-overwrites all of them — for pushing a freshly compiled version to an installed
machine, no network, no uninstall/reinstall.

Flow: stop service + clear firewall → back up kernel/UI (for rollback) → force-replace
kernel/geo/rules/UI → refresh CLI/man/systemd units → validate + restart → explicitly re-mount
firewall → health verification.
Any failing step rolls back the kernel/UI (the CLI is a management tool and is not rolled back;
config.yaml is always kept untouched).
To switch transparent-proxy mode use panixy mode (mode is data, redeploy does not touch it).`,
		Example: `  sudo ./panixy redeploy              # run from the root of the extracted new offline package
  sudo ./panixy redeploy --dry-run    # dry-run: preview the files to replace and the decisions`,
		RunE: runRedeploy,
	}
	addDryRunFlag(c, "dry-run mode: preview placement and decisions, do not execute")
	return c
}

func runRedeploy(cmd *cobra.Command, args []string) error {
	if dry, _ := cmd.Flags().GetBool("dry-run"); dry {
		return redeployDryRun(cmd)
	}
	return withRootLock(func(p paths.Paths) error { return runRedeployBody(p, cmd, args) })
}

func runRedeployBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	// Precheck: must already be installed (fresh install uses deploy); offline package assets complete; no bash legacy residue.
	if !exists(p.Bin) || !exists(p.Conf) || !exists(p.Cli) {
		return fmt.Errorf("no installed %s detected (kernel/config/CLI missing) — for a fresh install use sudo ./%s deploy", constants.ProgName, constants.ProgName)
	}
	pkgDir, err := os.Getwd() // redeploy must be run from the root of the extracted offline package
	if err != nil {
		return err
	}
	assets := filepath.Join(pkgDir, "assets")
	if _, err := os.Stat(assets); err != nil {
		return fmt.Errorf("no offline assets in the current directory (%s) — redeploy must run inside the extracted %s offline package", assets, constants.ProgName)
	}
	if legacy := systemdunit.DetectLegacy(p); legacy != "" {
		return fmt.Errorf("bash legacy deployment residue detected: %s\nclean it up manually first (see README \"Migrating from the bash version\")", legacy)
	}
	mode := statemode.Read(p.State)
	if mode == "" {
		mode = "tun"
	}
	logx.Info("redeploy started: in-place program file refresh (mode %s, config.yaml kept untouched)", mode)

	// [1] Stop the service and explicitly clear the firewall (the new binary may have changed FW rules, can't rely only on restart's ExecStartPost).
	logx.Step("[1/6] stop service and clear firewall rules")
	systemdunit.Stop()
	if fw, err := firewall.New(); err == nil {
		if err := fw.Teardown(); err != nil {
			logx.Warn("firewall cleanup failed: %v (fw apply will cover it after restart)", err)
		}
	}

	// [2] Back up kernel/UI (rollback the data plane on failure; geo/rules are static data, no rollback needed).
	logx.Step("[2/6] back up current kernel and UI")
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
		logx.Warn("redeploy failed, rolled back kernel/UI and restarted the old service (CLI stays at the new version, config untouched)")
	}

	// [3] Force-replace program files.
	logx.Step("[3/6] force-replace kernel/geo/rules/UI")
	if err := placeCoreForce(p, assets); err != nil {
		rollback()
		return err
	}
	placeGeoAndRulesForce(p, assets)
	if err := placeUIForce(p, assets); err != nil {
		rollback()
		return err
	}

	// [4] Refresh CLI/man/systemd units/sysctl.
	logx.Step("[4/6] refresh CLI/man/systemd units/sysctl")
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
	// Refresh the clean default-template copy (in sync with the current template/mode/secret, no subscription), for merge-conf to rebuild its baseline.
	if err := writeDefaultConf(p, mode, mihomoapi.NewFromConf(p.Conf).Secret); err != nil {
		logx.Warn("refreshing the default config copy failed: %v", err)
	}

	// [5] Validate + bring up the service.
	logx.Step("[5/6] validate config and restart service")
	if out, err := mihomoTest(p, p.Conf); err != nil {
		rollback()
		return fmt.Errorf("config validation failed (%s), rolled back", firstErrLine(out))
	}
	if err := systemdunit.PortCheck(p.Conf); err != nil {
		rollback()
		return err
	}
	if err := systemdunit.EnableNow(); err != nil {
		rollback()
		return fmt.Errorf("service failed to start, rolled back")
	}
	if err := systemdunit.EnableTimer(); err != nil {
		rollback()
		return fmt.Errorf("upgrade timer enable failed, rolled back")
	}

	// [6] Explicitly re-mount the firewall (new rules) + health verification (redundant with ExecStartPost but guarantees first-class status).
	logx.Step("[6/6] redeploy firewall and health verification")
	if err := applyFW(mode); err != nil {
		rollback()
		return fmt.Errorf("firewall deploy failed, rolled back: %v", err)
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		rollback()
		return fmt.Errorf("health verification timed out, rolled back")
	}

	// Success cleanup: remove UI backup, prune kernel backups, update the upgrade timestamp.
	os.RemoveAll(p.UiDir + ".old")
	pruneCoreBackups(p, constants.CoreKeep)
	os.WriteFile(p.LastUp, []byte(time.Now().Format("2006-01-02 15:04:05")+"\n"), 0o644)
	logx.Info("redeploy complete v%s: kernel/geo/rules/UI/CLI refreshed, firewall re-mounted, config kept", constants.Version)
	return nil
}

// redeployDryRun is a dry-run: verify assets, decisions, and the list of files to be replaced.
func redeployDryRun(cmd *cobra.Command) error {
	p := paths.Get()
	logx.Info("== redeploy --dry-run (dry-run mode, does not execute) ==")
	if !exists(p.Bin) || !exists(p.Conf) || !exists(p.Cli) {
		logx.Warn("no installed %s detected; for a fresh install use sudo ./%s deploy", constants.ProgName, constants.ProgName)
	}
	pkgDir, _ := os.Getwd()
	assets := filepath.Join(pkgDir, "assets")
	logx.Step("[precheck] offline package assets (%s)", assets)
	for _, item := range []struct{ name, path string }{
		{"kernel (" + runtimeArch() + ")", filepath.Join(assets, "core")},
		{"GeoIP.dat", filepath.Join(assets, "geo", "GeoIP.dat")},
		{"GeoSite.dat", filepath.Join(assets, "geo", "GeoSite.dat")},
		{"Country.mmdb", filepath.Join(assets, "geo", "Country.mmdb")},
		{"ad rules", filepath.Join(assets, "rule", "HyperADRules-Ads.yaml")},
		{"web UI", filepath.Join(assets, "ui", "official", "index.html")},
	} {
		if exists(item.path) {
			logx.Info("  ✓ %s", item.name)
		} else {
			logx.Warn("  ✗ %s missing", item.name)
		}
	}
	mode := statemode.Read(p.State)
	if mode == "" {
		mode = "tun"
	}
	logx.Step("[plan] force-replace: kernel/geo/rules/UI/CLI/man/units → clear FW → restart → re-mount FW (mode %s)", mode)
	logx.Info("kept untouched: %s (subscription/node selection/groups) and %s", p.Conf, p.Proxies)
	logx.Info("== dry-run done. Real run: sudo ./%s redeploy", constants.ProgName)
	return nil
}
