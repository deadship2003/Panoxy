// Package systemdunit 渲染/安装/管理 systemd 单元,并检测 bash 旧版部署残留。
package systemdunit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deadship2003/panixy/internal/asset"
	"github.com/deadship2003/panixy/internal/execx"
	"github.com/deadship2003/panixy/internal/paths"
)

// Render 生成三份单元内容(service 依赖当前模式)。
func Render(p paths.Paths, mode string) (map[string]string, error) {
	svc, err := asset.RenderService(asset.UnitData{
		Mode:  mode,
		Bin:   p.Bin,
		Conf:  p.Conf,
		Root:  p.Root,
		UiDir: p.UiDir,
		Cli:   p.Cli,
	})
	if err != nil {
		return nil, err
	}
	us, err := asset.RenderUpgradeService(p.Cli)
	if err != nil {
		return nil, err
	}
	ut, err := asset.RenderUpgradeTimer()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"panixy.service":         svc,
		"panixy-upgrade.service": us,
		"panixy-upgrade.timer":   ut,
	}, nil
}

// Write 写入单元并 daemon-reload。
func Write(p paths.Paths, mode string) error {
	units, err := Render(p, mode)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.UnitDir, 0o755); err != nil {
		return fmt.Errorf("创建单元目录失败: %w", err)
	}
	for name, content := range units {
		dst := filepath.Join(p.UnitDir, name)
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", dst, err)
		}
	}
	_, _ = execx.Run("systemctl", "daemon-reload")
	return nil
}

// Remove 删除单元并 daemon-reload(幂等)。
func Remove(p paths.Paths) {
	for _, name := range []string{"panixy.service", "panixy-upgrade.service", "panixy-upgrade.timer"} {
		os.Remove(filepath.Join(p.UnitDir, name))
	}
	_, _ = execx.Run("systemctl", "daemon-reload")
}

// IsActive 以退出码判断(--quiet 无输出)。
func IsActive() bool {
	_, err := execx.Run("systemctl", "is-active", "--quiet", "panixy.service")
	return err == nil
}

// Active 返回服务状态字符串(active/inactive/failed...)。
func Active() string {
	out, _ := execx.Run("systemctl", "is-active", "panixy.service")
	return strings.TrimSpace(out)
}

// EnableNow / DisableNow / Restart / Stop 服务生命周期。
func EnableNow() error {
	_, err := execx.RunOK("启用服务", "systemctl", "enable", "--now", "panixy.service")
	return err
}
func EnableTimer() error {
	_, err := execx.RunOK("启用升级 timer", "systemctl", "enable", "--now", "panixy-upgrade.timer")
	return err
}
func Restart() error {
	_, err := execx.RunOK("重启服务", "systemctl", "restart", "panixy.service")
	return err
}
func Stop() {
	_, _ = execx.Run("systemctl", "disable", "--now", "panixy.service", "panixy-upgrade.timer")
}

// DetectLegacy 检测 bash 旧版部署残留:旧 unit 含 resolvectl、或配置含 tun dns-hijack。
// 返回非空字符串即检测到的残留描述(deploy 据此中止并给手动清理指引)。
func DetectLegacy(p paths.Paths) string {
	if b, err := os.ReadFile(filepath.Join(p.UnitDir, "panixy.service")); err == nil {
		if strings.Contains(string(b), "resolvectl") {
			return "systemd 单元含 resolvectl(bash 旧版部署)"
		}
	}
	if b, err := os.ReadFile(p.Conf); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "#") {
				continue // 注释里的字样不算(新模板注释会提及历史字段)
			}
			if strings.HasPrefix(t, "dns-hijack:") {
				return "/etc/clash.yaml 含 tun dns-hijack(bash 旧版配置)"
			}
		}
	}
	return ""
}
