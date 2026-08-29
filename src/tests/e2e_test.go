// Package tests 是 Go 版 e2e:以环境变量覆盖 + 假 systemctl + 真实 mihomo 内核
// 驱动编译出的 panixy 二进制,覆盖 deploy/sub import/del/mode 的全事务链路。
//
// 安全约束:本机(开发机)不引导 tun 实例(auto-route 会改宿主路由表)——
// e2e 配置一律去除 tun 段;tun/tproxy 的真机引导验证在网关阶段进行。
package tests

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/deadship2003/panixy/internal/asset"
)

var (
	bin     string // 编译出的 panixy
	mihomo  string // 真实内核
	goTool  string
	e2eSkip bool
)

func TestMain(m *testing.M) {
	var err error
	goTool, err = exec.LookPath("go")
	if err != nil {
		fmt.Println("SKIP: 无 go 工具链")
		os.Exit(0)
	}
	mihomo = os.Getenv("MIHOMO_BIN")
	if mihomo == "" {
		if _, err := os.Stat("/opt/panixy/bin/mihomo"); err == nil {
			mihomo = "/opt/panixy/bin/mihomo"
		}
	}
	if mihomo == "" {
		fmt.Println("SKIP: 无 mihomo 内核(MIHOMO_BIN 可指定)")
		os.Exit(0)
	}
	// geo 来源:GEO_SRC > /opt/panixy > 离线包资产(机器清理过 /opt 后仍可测)
	if os.Getenv("GEO_SRC") == "" {
		for _, c := range []string{"/opt/panixy", homeDir() + "/panixy-e2e", "Panixy-V0.1.0-local-amd64/assets/geo"} {
			if _, err := os.Stat(filepath.Join(c, "GeoSite.dat")); err == nil {
				os.Setenv("GEO_SRC", c)
				break
			}
		}
	}
	dir, err := os.MkdirTemp("", "panixy-e2e-bin-")
	if err != nil {
		os.Exit(1)
	}
	bin = filepath.Join(dir, "panixy")
	out, err := exec.Command(goTool, "build", "-o", bin, "../cmd/panixy").CombinedOutput()
	if err != nil {
		fmt.Printf("SKIP: 构建 panixy 失败(依赖未拉取?): %s\n%s", err, out)
		os.Exit(0)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// env 是一套沙箱:路径覆盖 + 假 systemctl/ip/sysctl + 随机端口。
type env struct {
	t       *testing.T
	dir     string
	root    string
	conf    string
	apiPort int
	mixPort int
	dnsPort int
}

func homeDir() string { h, _ := os.UserHomeDir(); return h }

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{
		t: t, dir: dir,
		root:    filepath.Join(dir, "root"),
		conf:    filepath.Join(dir, "clash.yaml"),
		apiPort: freePort(t), mixPort: freePort(t), dnsPort: freePort(t),
	}
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	// 假 systemctl:restart/enable 启动沙箱内核(pid 按 ROOT 区分),is-active 按 pid 判断
	shim := filepath.Join(dir, "bin", "systemctl")
	pidf := filepath.Join(dir, "pid")
	os.WriteFile(shim, []byte(fmt.Sprintf(`#!/bin/sh
PIDF=%s
start_mh() {
  nohup "$PANIXY_ROOT/bin/mihomo" -f "$PANIXY_CONF" -d "$PANIXY_ROOT" >> "$PANIXY_ROOT/run.log" 2>&1 9>&- &
  echo $! >> "$PIDF"
}
case "$1" in
  restart) while read p; do kill "$p" 2>/dev/null; done < "$PIDF" 2>/dev/null; : > "$PIDF"; sleep 1; start_mh ;;
  enable)  [ "$2" = "--now" ] && [ "$3" = panixy.service ] && start_mh ;;
  disable) while read p; do kill "$p" 2>/dev/null; done < "$PIDF" 2>/dev/null; : > "$PIDF" ;;
  is-active) alive=0; while read p; do kill -0 "$p" 2>/dev/null && alive=1; done < "$PIDF" 2>/dev/null;
             [ "$alive" = 1 ] && echo active || { echo inactive; exit 3; } ;;
esac
exit 0
`, pidf)), 0o755)
	for _, name := range []string{"ip", "sysctl"} {
		os.WriteFile(filepath.Join(dir, "bin", name), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	// 测试结束回收沙箱内核进程,防泄漏
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidf); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				if l != "" {
					syscall.Kill(atoi(l), syscall.SIGKILL)
				}
			}
		}
	})
	return e
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (e *env) envOf() []string {
	return append(os.Environ(),
		"PATH="+filepath.Join(e.dir, "bin")+":"+os.Getenv("PATH"),
		"PANIXY_ROOT="+e.root,
		"PANIXY_CONF="+e.conf,
		"PANIXY_UNIT_DIR="+filepath.Join(e.dir, "units"),
		"PANIXY_CLI="+filepath.Join(e.dir, "cli", "panixy"),
		"PANIXY_MAN="+filepath.Join(e.dir, "man", "panixy.1.gz"),
		"PANIXY_STATE="+filepath.Join(e.dir, "state.yaml"),
		"PANIXY_SYSCTL="+filepath.Join(e.dir, "99.conf"),
		"PANIXY_LOCK="+filepath.Join(e.dir, "lock"),
		fmt.Sprintf("PANIXY_API_PORT=%d", e.apiPort),
		fmt.Sprintf("PANIXY_PROXY_PORT=%d", e.mixPort),
		"PANIXY_SECRET=e2esecret",
		"PANIXY_ALLOW_NONROOT=1",
		"PANIXY_SKIP_TPROXY_PROBE=1",
	)
}

func (e *env) cmd(args ...string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.Env = e.envOf()
	return cmd
}

// shim 直调假 systemctl(启动/停止沙箱内核)。
func (e *env) shim(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(e.dir, "bin", "systemctl"), args...)
	cmd.Env = e.envOf()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shim %v 失败: %s", args, out)
	}
}

func (e *env) run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := e.cmd(args...).CombinedOutput()
	if err != nil {
		t.Fatalf("panixy %v 失败:\n%s", args, out)
	}
	return string(out)
}

func (e *env) runFail(t *testing.T, args ...string) string {
	t.Helper()
	out, err := e.cmd(args...).CombinedOutput()
	if err == nil {
		t.Fatalf("panixy %v 竟然成功:\n%s", args, out)
	}
	return string(out)
}

// apiURL 直查沙箱内核 API。
func (e *env) apiURL(path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", e.apiPort, path)
}

// waitAPI 等沙箱内核 API 就绪。
func (e *env) waitAPI(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(e.apiURL("/version"))
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("沙箱内核 API 未就绪")
}

// noTunConf 渲染模板并去掉 tun 段(开发机不引导 tun)+ 换端口 + 固定密钥。
func noTunConf(t *testing.T, api, mix, dns int, tproxy bool) string {
	t.Helper()
	d := asset.DefaultConfigData()
	d.ApiPort, d.MixedPort, d.DnsPort = api, mix, dns
	d.TProxy = tproxy
	d.Secret = "e2esecret"
	out, err := asset.RenderConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	// 随机化模板写死的 http/socks 监听端口(本机若已装 panixy,固定 6666/6699 会撞真实网关)
	out = strings.Replace(out, "port: 6666", fmt.Sprintf("port: %d", freePort(t)), 1)
	out = strings.Replace(out, "socks-port: 6699", fmt.Sprintf("socks-port: %d", freePort(t)), 1)
	// 去 tun 段(tun: 到下一个顶层键)
	var b strings.Builder
	skip := false
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "tun:") {
			skip = true
			continue
		}
		if skip {
			if l != "" && l[0] != ' ' && l[0] != '#' {
				skip = false
			} else {
				continue
			}
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

// fakeSubServer 模拟机场:任意路径返回 n 节点 Clash YAML。
func fakeSubServer(t *testing.T, nodes int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString("proxies:\n")
		for i := 0; i < nodes; i++ {
			fmt.Fprintf(&b, "  - name: 'e2e-%d'\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\n", i)
		}
		w.Write([]byte(b.String()))
	}))
	t.Cleanup(srv.Close)
	return srv
}
