package asset

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderConfigVariants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tproxy bool
	}{{"tun", false}, {"tproxy", true}} {
		d := DefaultConfigData()
		d.TProxy = tc.tproxy
		d.Secret = "test-secret"
		out, err := RenderConfig(d)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !strings.Contains(out, "mixed-port: 33833") || !strings.Contains(out, "secret: test-secret") {
			t.Errorf("%s: 端口/密钥未渲染", tc.name)
		}
		if !strings.Contains(out, "routing-mark: 6666") {
			t.Errorf("%s: 缺 routing-mark(防 DNS 回环)", tc.name)
		}
		if !strings.Contains(out, "listen: 0.0.0.0:1053") {
			t.Errorf("%s: DNS 监听应为 0.0.0.0:1053(redirect 落点)", tc.name)
		}
		// 断言只看非注释行(注释里会提到这些历史字段)
		var body []string
		for _, l := range strings.Split(out, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "#") {
				body = append(body, l)
			}
		}
		code := strings.Join(body, "\n")
		if strings.Contains(code, "dns-hijack") || strings.Contains(code, "\n  fallback:") {
			t.Errorf("%s: 不应包含 dns-hijack/fallback", tc.name)
		}
		if tc.tproxy {
			if !strings.Contains(out, "tproxy-port: 7893") || strings.Contains(out, "tun:") {
				t.Errorf("tproxy 变体错误")
			}
		} else {
			if !strings.Contains(out, "stack: system") || strings.Contains(out, "tproxy-port") {
				t.Errorf("tun 变体错误")
			}
		}
	}
}

// TestConfigPassesMihomoCheck 用真实 mihomo 二进制 -t 校验两个变体(模板定稿的硬门槛)。
// 本机无内核时跳过(CI 打包阶段会再验)。
func TestConfigPassesMihomoCheck(t *testing.T) {
	bin := os.Getenv("MIHOMO_BIN")
	if bin == "" {
		if _, err := os.Stat("/opt/panixy/bin/mihomo"); err == nil {
			bin = "/opt/panixy/bin/mihomo"
		} else {
			t.Skip("本机无 mihomo 内核,跳过 -t 实测")
		}
	}
	geoSrc := os.Getenv("GEO_SRC")
	if geoSrc == "" {
		geoSrc = "/opt/panixy"
		if _, err := os.Stat(geoSrc + "/GeoSite.dat"); err != nil {
			h, _ := os.UserHomeDir()
			if _, err2 := os.Stat(h + "/panixy-e2e/GeoSite.dat"); err2 == nil {
				geoSrc = h + "/panixy-e2e"
			}
		}
	}
	for _, tc := range []struct {
		name   string
		tproxy bool
	}{{"tun", false}, {"tproxy", true}} {
		d := DefaultConfigData()
		d.TProxy = tc.tproxy
		out, err := RenderConfig(d)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		dir := t.TempDir()
		// geo 文件与 ui 目录(mihomo 要求 external-ui 在家目录内)
		for _, f := range []string{"GeoIP.dat", "GeoSite.dat", "Country.mmdb"} {
			b, err := os.ReadFile(filepath.Join(geoSrc, f))
			if err == nil {
				os.WriteFile(filepath.Join(dir, f), b, 0o644)
			}
		}
		os.MkdirAll(filepath.Join(dir, "ui", "official"), 0o755)
		conf := filepath.Join(dir, "clash.yaml")
		os.WriteFile(conf, []byte(out), 0o644)
		cmd := exec.Command(bin, "-t", "-f", conf, "-d", dir)
		got, err := cmd.CombinedOutput() // 教训:mihomo 日志走 stdout,必须合并捕获
		if err != nil {
			t.Errorf("%s 变体未过 mihomo -t:\n%s", tc.name, string(got))
		}
	}
}
