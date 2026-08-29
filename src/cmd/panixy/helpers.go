package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/deadship2003/panixy/internal/asset"
	"github.com/deadship2003/panixy/internal/execx"
	"github.com/deadship2003/panixy/internal/firewall"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
	"github.com/deadship2003/panixy/internal/paths"
)

func firewallNew() (firewall.Firewall, error) { return firewall.New() }

// runCmd 是 execx.Run 的简写。
func runCmd(name string, args ...string) string {
	out, _ := execx.Run(name, args...)
	return out
}

// writeDefaultConf 渲染纯净默认模板副本到 p.DefaultConf(config.default.yaml):
// 与 /etc/clash.yaml 初始渲染同源、保留 SUB_URL_PLACEHOLDER 且不含任何订阅,
// 供 merge-conf 或手工从干净基线重建配置。mode 决定 tun/tproxy 变体,secret 对齐当前密钥。
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

// checkTproxySupport 探测内核 xt_TPROXY 可用性(不可用则拒绝切换到 tproxy)。
func checkTproxySupport() error {
	probe := `grep -w TPROXY /proc/net/ip_tables_targets 2>/dev/null || { modprobe xt_TPROXY 2>/dev/null; grep -w TPROXY /proc/net/ip_tables_targets 2>/dev/null; }`
	out, _ := execx.RunShell(probe)
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("内核缺少 xt_TPROXY 模块(/proc/net/ip_tables_targets 无 TPROXY)")
	}
	return nil
}

// gunzipTo 解压 .gz 到目标文件。
func gunzipTo(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("内核压缩包损坏: %w", err)
	}
	defer zr.Close()
	os.MkdirAll(filepath.Dir(dst), 0o755)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, zr); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}
	return nil
}

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

func randRead(b []byte) { _, _ = rand.Read(b) }

// installMan 生成并安装全部 man 手册(根页 + 每个子命令页,与 --help 同源):
// man panixy / man panixy-init / man panixy-set-sub ...
func installMan(manGz, self string) {
	dir, err := os.MkdirTemp("", "panixy-man-")
	if err != nil {
		return
	}
	defer os.RemoveAll(dir)
	hdr := &doc.GenManHeader{Title: "PANIXY", Section: "1", Manual: "Panixy 手册"}
	if err := genAllMan(newRootForMan(), hdr, dir); err != nil {
		logx.Debug("man 生成失败(跳过): %v", err)
		return
	}
	files, _ := filepath.Glob(dir + "/panixy*.1")
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
	return execx.Run("journalctl", "-u", "panixy.service", "-u", "panixy-upgrade.service", "-n", n, "--no-pager")
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

// newRootForMan 重建命令树供手册生成(与 main 同构)。
func newRootForMan() *cobra.Command { return NewRootCmd() }
