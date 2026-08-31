package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/asset"
	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/execx"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/mihomoapi"
	"github.com/deadship2003/Panoxy/internal/paths"
)

// runCmd is shorthand for execx.Run.
func runCmd(name string, args ...string) string {
	out, _ := execx.Run(name, args...)
	return out
}

// writeDefaultConf renders a clean default-template copy to p.DefaultConf (config.default.yaml):
// same source as the initial /etc/clash.yaml render, keeps SUB_URL_PLACEHOLDER and contains no
// subscription, for merge-conf or manual rebuild from a clean baseline. mode selects the tun/tproxy
// variant, secret aligns with the current secret.
func writeDefaultConf(p paths.Paths, mode, secret string) error {
	d := asset.DefaultConfigData()
	d.TProxy = mode == "tproxy"
	d.Secret = secret
	out, err := asset.RenderConfig(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.DefaultConf), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p.DefaultConf, []byte(out), 0o644)
}

func runtimeArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	}
	return ""
}

// copyFile copies a file (small files, read at once).
func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	return os.WriteFile(dst, b, 0o644)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// installMan generates and installs all man pages (root page + one per subcommand, same source as --help):
// man panixy / man panixy-init / man panixy-sub-import ...
func installMan(manGz, self string) {
	dir, err := os.MkdirTemp("", constants.ProgName+"-man-")
	if err != nil {
		return
	}
	defer os.RemoveAll(dir)
	hdr := manHeader()
	if err := genAllMan(newRootForMan(), hdr, dir); err != nil {
		logx.Debug("man generation failed (skipping): %v", err)
		return
	}
	files, _ := filepath.Glob(dir + "/" + constants.ProgName + "*.1")
	dstDir := filepath.Dir(manGz)
	os.MkdirAll(dstDir, 0o755)
	for _, f := range files {
		base := filepath.Base(f)
		dst := filepath.Join(dstDir, strings.TrimSuffix(base, ".1")+".1.gz")
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write(b)
		zw.Close()
		os.WriteFile(dst, buf.Bytes(), 0o644)
	}
}

func journal(n string) (string, error) {
	return execx.Run("journalctl", "-u", constants.ProgName+".service", "-u", constants.ProgName+"-upgrade.service", "-n", n, "--no-pager")
}

func upgradeVerRe(s string) string {
	for _, f := range strings.Fields(s) {
		if strings.HasPrefix(f, "v") && len(f) > 2 && f[1] >= '0' && f[1] <= '9' {
			return f
		}
	}
	return ""
}

func probeUI(p string) string {
	api := mihomoapi.NewFromConf(p)
	code, err := api.RawGet("/ui/")
	if err != nil {
		return "000"
	}
	return code
}

func runExtractTgz(tgz, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	out, err := execx.Run("tar", "xzf", tgz, "-C", dst)
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// newRootForMan rebuilds the command tree for man generation (structurally identical to main).
func newRootForMan() *cobra.Command { return NewRootCmd() }
