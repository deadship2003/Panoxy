package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deadship2003/panixy/internal/asset"
)

// renderTmp 渲染基础模板到临时文件并返回 Editor。
func renderTmp(t *testing.T) (*Editor, string) {
	t.Helper()
	out, err := asset.RenderConfig(asset.DefaultConfigData())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "clash.yaml")
	if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

func TestRoundTripPreservesCommentsAndAnchors(t *testing.T) {
	e, p := renderTmp(t)
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	for _, want := range []string{
		"# ============ 订阅源",      // 注释保留
		"SUB_URL_PLACEHOLDER",     // 原内容保留
		"<<: *p",                  // merge 锚点保留
		"p: &p",                   // 锚点定义保留
		"- {name: dns, <<: *use,", // flow 组保留
		"🔃 自动选择",                  // emoji 不被转义成 \U 形式
		"stack: system",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("round-trip 丢失: %q", want)
		}
	}
}

func TestSetProviderAddAndWire(t *testing.T) {
	e, p := renderTmp(t)
	if err := e.SetProvider("airport2", "https://example.com/s2?token=a&sid=b", "./proxies/airport2.yaml"); err != nil {
		t.Fatal(err)
	}
	if n := e.WireProvider("airport2", true, nil); n != 3 {
		t.Fatalf("期望融合 3 个锚点持有者,实际 %d", n)
	}
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	s := string(mustRead(t, p))
	for _, want := range []string{
		"airport2:",
		"url: https://example.com/s2?token=a&sid=b",
		"path: ./proxies/airport2.yaml",
		"use: [SUB, airport2]",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("缺少: %q", want)
		}
	}
	if got := e.Providers(); len(got) != 2 || got[1] != "airport2" {
		t.Errorf("providers = %v", got)
	}
}

func TestSetProviderUpdateOnlyUrlPath(t *testing.T) {
	e, _ := renderTmp(t)
	// 覆盖 SUB:仅 url 变化,path 语义上仍是 ./proxies/SUB.yaml
	if err := e.SetProvider("SUB", "https://new.example.com/x", "./proxies/SUB.yaml"); err != nil {
		t.Fatal(err)
	}
	u, ok := e.ProviderURL("SUB")
	if !ok || u != "https://new.example.com/x" {
		t.Fatalf("url = %q %v", u, ok)
	}
	if got := e.Providers(); len(got) != 1 {
		t.Errorf("覆盖不应新增条目: %v", got)
	}
}

func TestRemoveProviderUnwires(t *testing.T) {
	e, p := renderTmp(t)
	e.SetProvider("airport2", "https://x/y", "./proxies/airport2.yaml")
	e.WireProvider("airport2", true, nil)
	if !e.RemoveProvider("airport2") {
		t.Fatal("删除失败")
	}
	if n := e.WireProvider("airport2", false, nil); n != 3 {
		t.Fatalf("期望反向融合 3 处,实际 %d", n)
	}
	e.Save()
	s := string(mustRead(t, p))
	if strings.Contains(s, "airport2") {
		t.Errorf("残留 airport2")
	}
	if !strings.Contains(s, "use: [SUB]") {
		t.Errorf("use 列表未还原")
	}
}

func TestAnchorGuard(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("mixed-port: 7890\n"), 0o644)
	e, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetProvider("X", "https://a/b", "./proxies/X.yaml"); err == nil {
		t.Fatal("无 &p 锚点应拒绝写入")
	}
}

func TestWireCustomConfigFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "custom.yaml")
	os.WriteFile(p, []byte(`mixed-port: 7890
proxy-providers:
  mine:
    type: http
    url: "https://a/b"
    path: ./proxies/mine.yaml
proxy-groups:
  - { name: G1, type: select, use: [mine] }
  - { name: G2, type: select, proxies: [DIRECT] }
rules:
  - MATCH,G1
`), 0o644)
	e, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := e.WireProvider("second", true, nil); n != 1 {
		t.Fatalf("自定义配置应只融合 use 非空的 G1,实际 %d", n)
	}
	e.Save()
	s := string(mustRead(t, p))
	if !strings.Contains(s, "use: [mine, second]") {
		t.Errorf("G1 未融合: %s", s)
	}
	if !strings.Contains(s, "proxies: [DIRECT]") {
		t.Errorf("G2 不应被改动")
	}
}

// TestEditedConfigPassesMihomoCheck 终极集成:模板 → 增删 provider → 真实内核 -t。
func TestEditedConfigPassesMihomoCheck(t *testing.T) {
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
	dir := t.TempDir()
	for _, f := range []string{"GeoIP.dat", "GeoSite.dat", "Country.mmdb"} {
		if b, err := os.ReadFile(filepath.Join(geoSrc, f)); err == nil {
			os.WriteFile(filepath.Join(dir, f), b, 0o644)
		}
	}
	os.MkdirAll(filepath.Join(dir, "ui", "official"), 0o755)

	e, _ := renderTmp(t)
	e.SetProvider("airport2", "https://example.com/s2", "./proxies/airport2.yaml")
	e.WireProvider("airport2", true, nil)
	// 删光全部订阅(airport2 与 SUB):组失去 use —— 预期 -t 拒绝
	e.RemoveProvider("airport2")
	e.WireProvider("airport2", false, nil)
	e.RemoveProvider("SUB")
	e.WireProvider("SUB", false, nil)
	e.path = filepath.Join(dir, "clash.yaml")
	e.Save()
	cmd := exec.Command(bin, "-t", "-f", e.path, "-d", dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("删光全部订阅应被 -t 拒绝,却通过了:\n%s", out)
	}
	if !strings.Contains(string(out), "use") {
		t.Logf("(-t 输出:%s)", out)
	}

	// 保留 SUB 的正常编辑必须通过
	e2, _ := renderTmp(t)
	e2.SetProvider("airport2", "https://example.com/s2", "./proxies/airport2.yaml")
	e2.WireProvider("airport2", true, nil)
	e2.path = filepath.Join(dir, "clash2.yaml")
	e2.Save()
	out2, err := exec.Command(bin, "-t", "-f", e2.path, "-d", dir).CombinedOutput()
	if err != nil {
		t.Errorf("编辑后的配置未过 -t:\n%s", out2)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
