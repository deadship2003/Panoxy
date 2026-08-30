// Package systemdunit 渲染/安装/管理 systemd 单元,并检测 bash 旧版部署残留。
package systemdunit

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"path/filepath"
	"strings"

	"github.com/deadship2003/Panoxy/internal/asset"
	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/execx"
	"github.com/deadship2003/Panoxy/internal/paths"
)

// 单元名随程序名派生(编译注入 ProgName 后自动跟随)。
var (
	unitMain    = constants.ProgName + ".service"
	unitUpgrade = constants.ProgName + "-upgrade.service"
	unitTimer   = constants.ProgName + "-upgrade.timer"
)

// Render 生成三份单元内容(service 依赖当前模式)。
func Render(p paths.Paths, mode string) (map[string]string, error) {
	svc, err := asset.RenderService(asset.UnitData{
		Mode:      mode,
		Prog:      constants.ProgName,
		EnvPrefix: constants.EnvPrefix(),
		Bin:       p.Bin,
		Conf:      p.Conf,
		Root:      p.Root,
		UiDir:     p.UiDir,
		Cli:       p.Cli,
	})
	if err != nil {
		return nil, err
	}
	us, err := asset.RenderUpgradeService(p.Cli, p.Root)
	if err != nil {
		return nil, err
	}
	ut, err := asset.RenderUpgradeTimer()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		unitMain:    svc,
		unitUpgrade: us,
		unitTimer:   ut,
	}, nil
}

// Write 写入单元并 daemon-reload。
func Write(p paths.Paths, mode string) error {
	units, err := Render(p, mode)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.UnitDir, 0o755); err != nil {
		return fmt.Errorf("failed to create the unit directory: %w", err)
	}
	for name, content := range units {
		dst := filepath.Join(p.UnitDir, name)
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", dst, err)
		}
	}
	_, _ = execx.Run("systemctl", "daemon-reload")
	return nil
}

// Remove 删除单元并 daemon-reload(幂等)。
func Remove(p paths.Paths) {
	for _, name := range []string{unitMain, unitUpgrade, unitTimer} {
		os.Remove(filepath.Join(p.UnitDir, name))
	}
	_, _ = execx.Run("systemctl", "daemon-reload")
}

// IsActive 服务是否处于 active 状态(由 Active 派生,单一事实源)。
func IsActive() bool { return Active() == "active" }

// Active 返回服务状态字符串(active/inactive/failed...)。
func Active() string {
	out, _ := execx.Run("systemctl", "is-active", unitMain)
	return strings.TrimSpace(out)
}

// EnableNow 启用并拉起服务;失败时附带 journal 尾部(真机排障的关键线索)。
func EnableNow() error {
	out, err := execx.Run("systemctl", "enable", "--now", unitMain)
	if err != nil {
		detail := ""
		if j, jerr := execx.Run("journalctl", "-u", unitMain, "-n", "15", "--no-pager"); jerr == nil && j != "" {
			detail = "\n── journalctl tail ──\n" + j
		}
		return fmt.Errorf("failed to enable the service: %s%s", strings.TrimSpace(out), detail)
	}
	return nil
}
func EnableTimer() error {
	_, err := execx.RunOK("enable upgrade timer", "systemctl", "enable", "--now", unitTimer)
	return err
}
func Restart() error {
	_, err := execx.RunOK("restart service", "systemctl", "restart", unitMain)
	return err
}
func Stop() {
	_, _ = execx.Run("systemctl", "disable", "--now", unitMain, unitTimer)
}

// DetectLegacy 检测 bash 旧版部署残留:旧 unit 含 resolvectl、或配置含 tun dns-hijack。
// 返回非空字符串即检测到的残留描述(deploy 据此中止并给手动清理指引)。
func DetectLegacy(p paths.Paths) string {
	if b, err := os.ReadFile(filepath.Join(p.UnitDir, unitMain)); err == nil {
		if strings.Contains(string(b), "resolvectl") {
			return "systemd unit contains resolvectl (bash legacy deployment)"
		}
	}
	if b, err := os.ReadFile(p.Conf); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "#") {
				continue // wording in comments does not count (the new template's comments mention historical fields)
			}
			if strings.HasPrefix(t, "dns-hijack:") {
				return "/etc/clash.yaml contains tun dns-hijack (bash legacy config)"
			}
		}
	}
	return ""
}

// PortCheck 启动前预检:从实际部署的配置解析监听端口,逐一检查占用。
// 旧实例未清理时新内核绑定失败是最常见的"服务启动失败"根因。
func PortCheck(confPath string) error {
	var c struct {
		MixedPort          int    `yaml:"mixed-port"`
		SocksPort          int    `yaml:"socks-port"`
		TproxyPort         int    `yaml:"tproxy-port"`
		ExternalController string `yaml:"external-controller"`
		DNS                struct {
			Listen string `yaml:"listen"`
		} `yaml:"dns"`
	}
	if b, err := os.ReadFile(confPath); err == nil {
		yaml.Unmarshal(b, &c) // 解析失败则跳过预检(不阻塞主流程)
	}
	why := map[int]string{
		c.MixedPort:                    "mixed-port",
		c.SocksPort:                    "socks-port",
		c.TproxyPort:                   "tproxy-port",
		portTail(c.ExternalController): "external-controller (web UI/API)",
		portTail(c.DNS.Listen):         "DNS listen",
	}
	var list []string
	for p, w := range why {
		if p <= 0 {
			continue
		}
		out, _ := execx.Run("sh", "-c",
			fmt.Sprintf("ss -tlnup 2>/dev/null | grep -qE ':%d\\b' && echo busy", p))
		if strings.TrimSpace(out) != "" {
			list = append(list, fmt.Sprintf("%d(%s)", p, w))
		}
	}
	if len(list) > 0 {
		hint := ""
		if pout, _ := execx.Run("sh", "-c", "pgrep -af 'bin/mihomo' | head -3"); strings.TrimSpace(pout) != "" {
			hint = "\nrunning mihomo detected:\n" + pout + "→ old deployment not cleaned up: first sudo " + constants.ProgName + " uninstall (old version) / stop the old instance, then deploy\n"
		}
		return fmt.Errorf("port already in use: %s%s", strings.Join(list, ", "), hint)
	}
	return nil
}

func portTail(addr string) int {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		n := 0
		for _, ch := range addr[i+1:] {
			if ch < '0' || ch > '9' {
				return 0
			}
			n = n*10 + int(ch-'0')
		}
		return n
	}
	return 0
}
