package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/deadship2003/Panoxy/internal/asset"
	"github.com/deadship2003/Panoxy/internal/config"
	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/health"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/mihomoapi"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/systemdunit"
)

// runCheck validates config with the kernel's -t flag (read-only, no root).
func runCheck(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	f := p.Conf
	if len(args) > 0 {
		f = args[0]
	}
	if _, err := os.Stat(f); err != nil {
		return fmt.Errorf("file does not exist: %s", f)
	}
	out, err := mihomoTest(p, f)
	fmt.Print(out)
	return err
}

// runApplyConf applies a custom config: prefer hot-reload (non-provider changes only!), fall back to restart, then restore on failure.
func runApplyConf(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error { return runApplyConfBody(p, cmd, args) })
}

func runApplyConfBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: %s apply-conf <yaml>", constants.ProgName)
	}
	src := args[0]
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("file does not exist: %s", src)
	}
	if out, err := mihomoTest(p, src); err != nil {
		return fmt.Errorf("file failed kernel validation (%s); system unchanged", firstErrLine(out))
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
	// Hot-reload does not refresh proxy-providers (mihomo limitation); only effective for non-provider changes.
	if err := api.ReloadConf(p.Conf); err == nil && health.WaitHealthy(p.Conf, 20*time.Second, "") == nil {
		config.ClearBackup(p.Conf)
		logx.Info("config hot-reloaded (no restart); note provider changes need a restart")
		return nil
	}
	logx.Step("hot-reload had no effect, falling back to restart")
	if err := systemdunit.Restart(); err != nil {
		rollbackRestart(p)
		return fmt.Errorf("restart failed, original config restored")
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		rollbackRestart(p)
		return fmt.Errorf("health check failed after restart, original config restored: %w", err)
	}
	config.ClearBackup(p.Conf)
	logx.Info("config applied: %s -> %s", src, p.Conf)
	return nil
}

// runUnits prints rendered unit text (offline review, does not touch the system).
func runUnits(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	units, err := systemdunit.Render(p, "tun")
	if err != nil {
		return err
	}
	for _, name := range []string{constants.ProgName + ".service", constants.ProgName + "-upgrade.service", constants.ProgName + "-upgrade.timer"} {
		fmt.Printf("===== %s =====\n%s\n", name, units[name])
	}
	return nil
}

// runConfig renders the default template to stdout; --write additionally writes config.default.yaml back (no deploy, does not touch the system).
func runConfig(cmd *cobra.Command, args []string) error {
	mode, _ := cmd.Flags().GetString("mode")
	if mode != "tun" && mode != "tproxy" {
		return fmt.Errorf("--mode must be tun or tproxy")
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
		logx.Info("default config written back to %s (mode %s, secret %s)", p.DefaultConf, mode, secret)
	}
	return nil
}

// runLog passes through to journalctl.
func runLog(cmd *cobra.Command, args []string) error {
	n := "80"
	if len(args) > 0 {
		n = args[0]
	}
	out, err := journal(n)
	fmt.Print(out)
	return err
}

// warnCompat runs a compatibility self-check before merging a personal config: mismatching
// any of the three firewall "contract" values causes real problems.
//
//	routing-mark 6666 —— the firewall exempts mihomo's own traffic by this mark; missing it causes a DNS loop
//	dns.listen 0.0.0.0:1053 —— the nft redirect target; mismatching it makes DNS hijack a no-op
//	tun.dns-hijack —— the firewall already hijacks DNS uniformly; keeping it double-processes
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
		logx.Warn("config is missing routing-mark: %d — the firewall will be unable to exempt mihomo's own traffic, possibly causing a DNS loop deadlock (the template ships it by default, do not remove)", constants.MarkSelf)
	}
	if !strings.Contains(c.DNS.Listen, ":1053") {
		logx.Warn("dns.listen=%q does not match the firewall hijack target (0.0.0.0:1053) — DNS hijack will be a no-op, please change it to 0.0.0.0:1053", c.DNS.Listen)
	}
	if len(c.TUN.DNSHijack) > 0 {
		logx.Warn("tun.dns-hijack still present — DNS hijack is already handled uniformly by the firewall, remove this item to avoid double hijacking")
	}
}
