package asset

import (
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
		if !strings.Contains(out, `listen: "[::]:1053"`) {
			t.Errorf("%s: DNS 监听应为 [::]:1053 双栈(redirect 落点)", tc.name)
		}
		if !strings.Contains(out, "fake-ip-range6: 2001:2::1/48") {
			t.Errorf("%s: 缺 fake-ip-range6(IPv6 fake-ip 池)", tc.name)
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
