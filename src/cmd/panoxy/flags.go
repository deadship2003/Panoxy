package main

import (
	"github.com/spf13/cobra"

	"github.com/deadship2003/panoxy/internal/constants"
)

// Unified registration of subscription and deployment parameters: init/deploy/try/sub import share the same
// flag set, keeping defaults and descriptions consistent to avoid drift from changing only one place (single source of truth).

// addSubSourceFlags is the subscription source: --name/--file.
func addSubSourceFlags(c *cobra.Command) {
	c.Flags().String("name", "SUB", "subscription provider name (only [a-zA-Z0-9_-])")
	c.Flags().String("file", "", "local subscription YAML file (skip the network fetch)")
}

// addDeployFlags is the deployment parameters: --proxy-mode/--secret.
func addDeployFlags(c *cobra.Command) {
	c.Flags().String("proxy-mode", "tun", "transparent proxy mode: tun | tproxy")
	c.Flags().String("secret", constants.DefSecret, "web UI/API secret")
}

// addDownloadFlags is the download fallback parameter: --mirror (shared by init/try which download over the network).
func addDownloadFlags(c *cobra.Command) {
	c.Flags().StringSlice("mirror", nil, "gh mirror prefixes (multiple allowed; third-party source)")
}

// addDryRunFlag is the dry-run parameter (each command's semantics differ slightly, described by desc).
func addDryRunFlag(c *cobra.Command, desc string) {
	c.Flags().Bool("dry-run", false, desc)
}
