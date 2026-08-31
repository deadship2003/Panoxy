package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/firewall"
	"github.com/deadship2003/Panoxy/internal/health"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/systemdunit"
)

// Service lifecycle commands: start/stop/restart the panixy service.
//
// The firewall is normally loaded/removed by the unit's ExecStartPost (fw apply) / ExecStop
// (fw teardown) hooks; these commands additionally do an explicit teardown + health check so a
// start/stop is verifiable rather than a blind systemctl pass-through, and so stale firewall
// rules can never survive a stop.

func cmdStart() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "start the service (enable on boot) and verify health",
		Long: `Start the panixy service, ensure it is enabled to auto-start on boot (and re-enable the
daily upgrade timer), then wait for the API to become healthy.

The service unit's ExecStartPost loads the firewall, so starting via systemd also restores the
DNS-hijack/TPROXY rules. Idempotent: running it while the service is already active just
re-ensures the upgrade timer and reports the current state.`,
		Example: "  sudo panixy start     # start and enable on boot",
		RunE:    func(cmd *cobra.Command, args []string) error { return runStart(cmd, args) },
	}
}

func cmdStop() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "stop the service (disable on boot) and clear the firewall",
		Long: `Stop the panixy service, disable it so it stays off across a reboot, stop the daily upgrade
timer, and explicitly tear down the firewall rules.

This is the temporary-off switch (everything is kept: config/subscriptions/data); run
sudo panixy start to bring it back. For a full removal use panixy uninstall.`,
		Example: "  sudo panixy stop      # stop and disable (firewall cleared)",
		RunE:    func(cmd *cobra.Command, args []string) error { return runStop(cmd, args) },
	}
}

func cmdRestart() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "restart the service (self-heals the firewall) and verify health",
		Long: `Restart the panixy service. The unit's ExecStop/ExecStartPost re-run the firewall teardown and
apply, so a restart also self-heals any stale rules left by kill -9/OOM. The service stays
enabled (restart does not change boot persistence).`,
		Example: "  sudo panixy restart   # restart (self-heals firewall)",
		RunE:    func(cmd *cobra.Command, args []string) error { return runRestart(cmd, args) },
	}
}

func runStart(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error {
		if err := requireInstalled(p); err != nil {
			return err
		}
		if systemdunit.IsActive() {
			// Already up: keep it enabled, just re-ensure the upgrade timer (idempotent).
			if err := systemdunit.EnableTimer(); err != nil {
				return err
			}
			logx.Info("service already active; kept enabled on boot (upgrade timer ensured)")
			return nil
		}
		logx.Step("start service (enable on boot) and re-enable the upgrade timer")
		if err := systemdunit.EnableNow(); err != nil {
			return err
		}
		if err := systemdunit.EnableTimer(); err != nil {
			return fmt.Errorf("upgrade timer enable failed: %w", err)
		}
		if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
			return fmt.Errorf("service started but health check timed out: %w", err)
		}
		logx.Info("service started and enabled on boot; firewall loaded; %s status to verify", constants.ProgName)
		return nil
	})
}

func runStop(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error {
		if err := requireInstalled(p); err != nil {
			return err
		}
		logx.Step("stop service (disable on boot) and clear firewall")
		systemdunit.Stop()
		// Explicit teardown in addition to the unit's ExecStop: a failed/crashed unit may not run
		// ExecStop, and we never want stale rules left behind a "stopped" gateway.
		if err := firewall.Teardown(); err != nil {
			logx.Warn("firewall teardown failed: %v (retry %s fw teardown)", err, constants.ProgName)
		}
		logx.Info("service stopped and disabled; firewall rules removed (%s start to resume)", constants.ProgName)
		return nil
	})
}

func runRestart(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error {
		if err := requireInstalled(p); err != nil {
			return err
		}
		logx.Step("restart service (unit re-loads the firewall)")
		if err := systemdunit.Restart(); err != nil {
			return err
		}
		if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
			return fmt.Errorf("service restarted but health check timed out: %w", err)
		}
		logx.Info("service restarted (firewall self-healed); %s status to verify", constants.ProgName)
		return nil
	})
}

// requireInstalled fails fast when the service unit has not been written (init/deploy have not run),
// instead of leaking systemctl's raw "unit not found".
func requireInstalled(p paths.Paths) error {
	if !systemdunit.Installed(p) {
		return fmt.Errorf("no installed %s service detected (missing %s); install first with sudo %s init/deploy",
			constants.ProgName, filepath.Join(p.UnitDir, constants.ProgName+".service"), constants.ProgName)
	}
	return nil
}
