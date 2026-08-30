package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deadship2003/Panoxy/internal/constants"
)

// e2e 主线:deploy(预置无tun配置)→ sub import 成功/失败/离线 → sub del → mode 配置级切换。

func TestE2EDeployWithPresetConf(t *testing.T) {
	e := newEnv(t)
	pkg := t.TempDir()
	buildAssets(t, pkg)
	// 预置配置(现有配置优先;无 tun,安全)
	os.WriteFile(e.conf, []byte(noTunConf(t, e.apiPort, e.mixPort, e.dnsPort, false)), 0o644)

	cmd := e.cmd("deploy", "--verbose")
	cmd.Dir = pkg
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("deploy 失败:\n%s", out)
	}
	for _, want := range []string{"kernel", "geo and ad rules", "web UI", "existing config detected"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("deploy 输出缺少 %q:\n%s", want, out)
		}
	}
	checkFile(t, filepath.Join(e.root, "bin", "mihomo"))
	checkFile(t, filepath.Join(e.root, "rule_provider", "HyperADRules-Ads.yaml"))
	checkFile(t, filepath.Join(e.root, "ui", "official", "index.html"))
	checkFile(t, filepath.Join(e.dir, "cli", constants.ProgName))
	checkFile(t, filepath.Join(e.dir, "man", constants.ProgName+".1.gz"))
	if b, _ := os.ReadFile(filepath.Join(e.dir, "state.yaml")); !strings.Contains(string(b), "tun") {
		t.Errorf("状态文件未写 proxy-mode=tun: %s", b)
	}
	e.waitAPI(t)
}

func TestE2ESetSubFlows(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(e.conf, []byte(noTunConf(t, e.apiPort, e.mixPort, e.dnsPort, false)), 0o644)
	bootSandbox(t, e) // 直接启动内核(sub import 自带重启,先有实例以验证节点数)
	srv := fakeSubServer(t, 4)

	// 1) 可达订阅:成功 + 节点数验证
	out := e.run(t, "sub", "import", "--name", "main", srv.URL+"/sub?token=ok&sid=x")
	if !strings.Contains(out, "loaded: 4 nodes") {
		t.Fatalf("未见节点数报告:\n%s", out)
	}
	if b, _ := os.ReadFile(e.conf); !strings.Contains(string(b), srv.URL+"/sub?token=ok&sid=x") {
		t.Fatal("URL 未写入配置(含 & 参数)")
	}
	if b, _ := os.ReadFile(filepath.Join(e.root, "proxies", "main.yaml")); !strings.Contains(string(b), "e2e-0") {
		t.Fatal("订阅缓存未预置")
	}
	if b, _ := os.ReadFile(e.conf); strings.Contains(string(b), `url: "SUB_URL_PLACEHOLDER"`) {
		t.Fatal("首个真实订阅导入后,占位订阅应自动退场(配置仍含占位 url)")
	}

	// 2) 不可达订阅:诚实失败 + 配置零改动
	before, _ := os.ReadFile(e.conf)
	out = e.runFail(t, "sub", "import", "http://192.0.2.1:9/dead")
	if !strings.Contains(out, "subscription fetch or validation failed") {
		t.Fatalf("报错不符:\n%s", out)
	}
	after, _ := os.ReadFile(e.conf)
	if string(before) != string(after) {
		t.Fatal("失败路径改动了配置")
	}

	// 3) 离线导入(--file,不联网)
	seed := filepath.Join(e.dir, "seed.yaml")
	os.WriteFile(seed, []byte("proxies:\n  - name: offline-x\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\n"), 0o644)
	out = e.run(t, "sub", "import", "--name", "backup", "--file", seed, "https://blocked.example.com/x?token=w")
	if !strings.Contains(out, "using local subscription file") {
		t.Fatalf("未见离线导入日志:\n%s", out)
	}

	// 4) sub list:两个订阅都在,单订阅状态可见
	out = e.run(t, "sub", "list")
	for _, want := range []string{"main", "backup"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sub list 缺 %s:\n%s", want, out)
		}
	}

	// 5) 删除最后一个订阅被 -t 拒绝(组失去 use),先删 backup 成功
	e.run(t, "sub", "del", "--name", "backup")
	if b, _ := os.ReadFile(e.conf); strings.Contains(string(b), "backup:") {
		t.Fatal("backup 未删除")
	}
}

func TestE2EModeSwitchConfigLevel(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(e.conf, []byte(noTunConf(t, e.apiPort, e.mixPort, e.dnsPort, false)), 0o644)
	bootSandbox(t, e)

	// tun → tproxy:配置出现 tproxy-port、消失 tun;state 写入;失败回滚链不触发
	out := e.run(t, "mode", "tproxy")
	if !strings.Contains(out, "tproxy") {
		t.Fatalf("mode 输出异常:\n%s", out)
	}
	b, _ := os.ReadFile(e.conf)
	if !strings.Contains(string(b), "tproxy-port: 7893") || strings.Contains(string(b), "\ntun:") {
		t.Fatalf("配置变体切换失败:\n%s", b)
	}
	if s, _ := os.ReadFile(filepath.Join(e.dir, "state.yaml")); !strings.Contains(string(s), "tproxy") {
		t.Fatalf("状态未更新: %s", s)
	}
	// tproxy → tun:恢复
	e.run(t, "mode", "tun")
	b, _ = os.ReadFile(e.conf)
	if !strings.Contains(string(b), "\ntun:") || strings.Contains(string(b), "tproxy-port") {
		t.Fatalf("配置未恢复 tun:\n%s", b)
	}
}

// bootSandbox 直接经 shim 启动内核(等价 enable --now)。
func bootSandbox(t *testing.T, e *env) {
	t.Helper()
	// 内核就位(deploy 的 placeCore 逻辑这里手动做)
	os.MkdirAll(filepath.Join(e.root, "bin"), 0o755)
	if b, err := os.ReadFile(mihomo); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(filepath.Join(e.root, "bin", "mihomo"), b, 0o755); err != nil {
		t.Fatal(err)
	}
	// geo + ui(-t 与启动需要)
	geoSrc := geoSrcOr(t)
	for _, f := range []string{"GeoIP.dat", "GeoSite.dat", "Country.mmdb"} {
		if b, err := os.ReadFile(filepath.Join(geoSrc, f)); err == nil {
			os.WriteFile(filepath.Join(e.root, f), b, 0o644)
		}
	}
	os.MkdirAll(filepath.Join(e.root, "ui", "official"), 0o755)
	e.shim(t, "enable", "--now", constants.ProgName+".service")
	e.waitAPI(t)
}

// buildAssets 组装迷你离线包(内核 gz/geo/UI/规则)。
func buildAssets(t *testing.T, pkg string) {
	t.Helper()
	for _, d := range []string{"assets/core", "assets/geo", "assets/ui/official", "assets/rule"} {
		os.MkdirAll(filepath.Join(pkg, d), 0o755)
	}
	// 内核 gzip
	gz := filepath.Join(pkg, "assets/core/mihomo-linux-amd64-v0.0.0-e2e.gz")
	if out, err := exec.Command("sh", "-c", "gzip -c "+mihomo+" > "+gz).CombinedOutput(); err != nil {
		t.Fatalf("内核打包失败: %s", out)
	}
	geoSrc := geoSrcOr(t)
	for _, f := range []string{"GeoIP.dat", "GeoSite.dat", "Country.mmdb"} {
		if b, err := os.ReadFile(filepath.Join(geoSrc, f)); err == nil {
			os.WriteFile(filepath.Join(pkg, "assets/geo", f), b, 0o644)
		}
	}
	os.WriteFile(filepath.Join(pkg, "assets/ui/official/index.html"), []byte("<html>e2e</html>"), 0o644)
	os.WriteFile(filepath.Join(pkg, "assets/rule/HyperADRules-Ads.yaml"), []byte("payload:\n  - '+.ad.example'\n"), 0o644)
}

func checkFile(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err != nil {
		t.Errorf("文件缺失: %s", p)
	}
}

func geoSrcOr(t *testing.T) string {
	t.Helper()
	if g := os.Getenv("GEO_SRC"); g != "" {
		return g
	}
	for _, c := range []string{
		filepath.Join("/opt", constants.ProgName),
		"/opt/panixy", // 旧版残留
		homeDir() + "/panixy-e2e",
	} {
		if _, err := os.Stat(filepath.Join(c, "GeoSite.dat")); err == nil {
			return c
		}
	}
	t.Fatal("缺 geo 数据(GEO_SRC 可指定,或放 ~/panixy-e2e)")
	return ""
}
