package main

import (
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/deadship2003/panixy/internal/execx"
	"github.com/deadship2003/panixy/internal/firewall"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
)

func firewallNew() (firewall.Firewall, error) { return firewall.New() }

// runCmd 是 execx.Run 的简写。
func runCmd(name string, args ...string) string {
	out, _ := execx.Run(name, args...)
	return out
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

// installMan 生成并压缩 man 手册(从当前二进制的 cobra 树,保证与 --help 同源)。
// 此处直接调用一次自身 `panixy man` 的生成逻辑等价物:复用 doc 生成在 main.go 内实现,
// 简化为调用自身子进程,避免引 doc 包循环。
func installMan(manGz, self string) {
	os.MkdirAll(filepath.Dir(manGz), 0o755)
	out, err := exec.Command(self, "man", "--raw").CombinedOutput()
	if err != nil {
		logx.Debug("man 生成失败(跳过): %v", err)
		return
	}
	os.WriteFile(manGz, out, 0o644)
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
