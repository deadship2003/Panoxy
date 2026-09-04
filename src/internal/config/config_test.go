package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deadship2003/panoxy/internal/asset"
	"github.com/deadship2003/panoxy/internal/core"
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
		"- {name: DNS, <<: *use,", // flow 组保留
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

// TestPruneDerivedKeepsOnlyMatchedGroups 校验派生组剪枝:按实际节点名剔除无命中的地区/类型组,
// 并从 pr/prd/dns 的 proxies 同步移除,避免悬空引用。
func TestPruneDerivedKeepsOnlyMatchedGroups(t *testing.T) {
	e, p := renderTmp(t)
	// 模拟一次真实订阅:仅 香港 + 美国 + 流媒体 节点
	names := []string{"香港 01 | 原生IP", "香港 02", "美国 流媒体解锁"}
	if n := e.PruneDerived(names); n == 0 {
		t.Fatal("应剔除无匹配的派生组")
	}
	e.Save()
	s := string(mustRead(t, p))
	for _, keep := range []string{"香港", "美国", "🎬 流媒体", "全部节点", "🔃 自动选择"} {
		if !strings.Contains(s, keep) {
			t.Errorf("应保留 %q,却缺失", keep)
		}
	}
	for _, gone := range []string{"阿根廷", "台湾", "🇨🇳 回国"} {
		if strings.Contains(s, gone) {
			t.Errorf("应剔除 %q,却仍残留", gone)
		}
	}
}

// TestEditedConfigPassesCheck 终极集成:模板 → 增删 provider → 进程内内核 -t(等价外部 mihomo -t)。
func TestEditedConfigPassesCheck(t *testing.T) {
	geoSrc := geoFallback(t)
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
	if err := core.Validate(dir, mustRead(t, e.path)); err == nil {
		t.Fatalf("删光全部订阅应被 -t 拒绝,却通过了")
	}

	// 保留 SUB 的正常编辑必须通过
	e2, _ := renderTmp(t)
	e2.SetProvider("airport2", "https://example.com/s2", "./proxies/airport2.yaml")
	e2.WireProvider("airport2", true, nil)
	e2.path = filepath.Join(dir, "clash2.yaml")
	e2.Save()
	if err := core.Validate(dir, mustRead(t, e2.path)); err != nil {
		t.Errorf("编辑后的配置未过 -t:%v", err)
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
