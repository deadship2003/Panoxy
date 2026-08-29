package subscribe

import (
	"encoding/base64"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const clashYAML = `proxies:
  - {name: "香港-01", type: trojan, server: 1.2.3.4, port: 443, password: x}
  - {name: "日本-01", type: vless, server: 5.6.7.8, port: 443, uuid: "53fac7ed-2b9e-43f6-ab96-9d37d4667f94", tls: true}
`

const uriList = `vless://53fac7ed-2b9e-43f6-ab96-9d37d4667f94@example.com:443?security=tls#香港-01
trojan://password123@example.com:443?sni=example.com#香港-02
ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@1.2.3.4:8388#日本-01
`

const singBox = `{"outbounds":[{"type":"vless","tag":"香港-01","server":"example.com","server_port":443,"uuid":"53fac7ed-2b9e-43f6-ab96-9d37d4667f94","tls":{"enabled":true,"server_name":"example.com"},"transport":{"type":"ws","path":"/x"}},{"type":"trojan","tag":"香港-02","server":"example.com","server_port":443,"password":"pw"},{"type":"direct","tag":"direct-out"}]}`

const surge = `#!MANAGED-CONFIG
[General]
loglevel = notify
[Proxy]
香港-01 = trojan, example.com, 443, password=password123, sni=example.com
香港-02 = ss, 1.2.3.4, 8388, encrypt-method=aes-256-gcm, password=pass
[Proxy Group]
P = select, 香港-01, 香港-02
[Rule]
FINAL,P
`

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Format
	}{
		{"clash", clashYAML, FormatClash},
		{"uri", uriList, FormatURI},
		{"b64-uri", b64(uriList), FormatBase64URI},
		{"b64-clash", b64(clashYAML), FormatBase64Clash},
		{"singbox", singBox, FormatSingBox},
		{"surge", surge, FormatSurge},
		{"html", "<html>登录失效</html>", FormatUnknown},
		{"empty", "", FormatUnknown},
	}
	for _, c := range cases {
		if got := Detect([]byte(c.in)); got != c.want {
			t.Errorf("Detect(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	// 原生格式:原样透传,不转换
	for _, in := range []string{clashYAML, uriList, b64(uriList)} {
		out, converted, err := Normalize([]byte(in))
		if err != nil {
			t.Fatalf("Normalize(原生格式) 出错: %v", err)
		}
		if converted {
			t.Errorf("原生格式不应转换: %q", firstNonEmptyLine(in))
		}
		if strings.TrimSpace(string(out)) != strings.TrimSpace(in) {
			t.Errorf("原生格式应原样透传,got=%q", string(out))
		}
	}

	// base64 Clash:解码 + converted=true
	out, converted, err := Normalize([]byte(b64(clashYAML)))
	if err != nil || !converted {
		t.Fatalf("b64-clash 应转换: converted=%v err=%v", converted, err)
	}
	if !strings.Contains(string(out), "proxies:") {
		t.Errorf("b64-clash 解码后应含 proxies:, got=%q", string(out))
	}

	// sing-box → Clash YAML
	out, converted, err = Normalize([]byte(singBox))
	if err != nil || !converted {
		t.Fatalf("sing-box 应转换: converted=%v err=%v", converted, err)
	}
	var doc struct {
		Proxies []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("sing-box 转换结果不是有效 YAML: %v", err)
	}
	if len(doc.Proxies) != 2 { // direct 出站应被跳过
		t.Fatalf("sing-box 应转换出 2 个节点,got=%d", len(doc.Proxies))
	}
	if doc.Proxies[0].Type != "vless" || doc.Proxies[1].Type != "trojan" {
		t.Errorf("sing-box 转换类型错误: %+v", doc.Proxies)
	}

	// Surge → Clash YAML
	out, converted, err = Normalize([]byte(surge))
	if err != nil || !converted {
		t.Fatalf("surge 应转换: converted=%v err=%v", converted, err)
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("surge 转换结果不是有效 YAML: %v", err)
	}
	if len(doc.Proxies) != 2 {
		t.Fatalf("surge 应转换出 2 个节点,got=%d", len(doc.Proxies))
	}

	// 未知格式报错
	if _, _, err := Normalize([]byte("<html>err</html>")); err == nil {
		t.Errorf("未知格式应报错")
	}
}

func TestNodeNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{clashYAML, []string{"香港-01", "日本-01"}},
		{uriList, []string{"香港-01", "香港-02", "日本-01"}},
		{b64(uriList), []string{"香港-01", "香港-02", "日本-01"}},
		{b64(clashYAML), []string{"香港-01", "日本-01"}},
		{singBox, []string{"香港-01", "香港-02", "direct-out"}},
		{surge, []string{"香港-01", "香港-02"}},
	}
	for _, c := range cases {
		got, err := NodeNames([]byte(c.in))
		if err != nil {
			t.Fatalf("NodeNames 出错: %v", err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("NodeNames(%q) = %v, want %v", firstNonEmptyLine(c.in), got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Fatalf("NodeNames(%q)[%d] = %q, want %q", firstNonEmptyLine(c.in), i, got[i], c.want[i])
			}
		}
	}
}

func TestValidate(t *testing.T) {
	for _, in := range []string{clashYAML, uriList, b64(uriList), b64(clashYAML), singBox, surge} {
		if err := Validate([]byte(in)); err != nil {
			t.Errorf("Validate(%q) 应通过, got=%v", firstNonEmptyLine(in), err)
		}
	}
	if err := Validate([]byte("")); err == nil {
		t.Errorf("空内容应报错")
	}
	if err := Validate([]byte("<html>登录失效</html>")); err == nil {
		t.Errorf("HTML 错误页应报错")
	}
	if err := Validate([]byte("{}")); err == nil {
		t.Errorf("无节点 JSON 应报错")
	}
	// 无 #name 的 URI 列表:按节点行计数,不应被误判为 0
	if err := Validate([]byte("vless://uuid@example.com:443?security=tls\n")); err != nil {
		t.Errorf("无名 URI 列表应通过(按节点计数), got=%v", err)
	}
}

// TestSingboxFieldMapping 校验转换器输出的关键字段(尤其 mihomo 的硬性要求):
// vmess 必须显式给 alterId 与 cipher;hysteria2/tuic 用 sni 而非 tls/servername。
func TestSingboxFieldMapping(t *testing.T) {
	in := `{"outbounds":[
{"type":"vmess","tag":"vm","server":"a.com","server_port":80,"uuid":"53fac7ed-2b9e-43f6-ab96-9d37d4667f94","alter_id":0,"security":"auto"},
{"type":"hysteria2","tag":"hy2","server":"c.com","server_port":443,"password":"pw","tls":{"enabled":true,"server_name":"c.com"}},
{"type":"tuic","tag":"tu","server":"d.com","server_port":443,"uuid":"53fac7ed-2b9e-43f6-ab96-9d37d4667f94","password":"pw"}
]}`
	out, _, err := Normalize([]byte(in))
	if err != nil {
		t.Fatalf("Normalize 出错: %v", err)
	}
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("输出非有效 YAML: %v", err)
	}
	byName := map[string]map[string]any{}
	for _, p := range doc.Proxies {
		byName[p["name"].(string)] = p
	}

	vm := byName["vm"]
	if vm["alterId"] != 0 || vm["cipher"] != "auto" {
		t.Errorf("vmess 必须显式给 alterId=0 与 cipher=auto, got alterId=%v cipher=%v", vm["alterId"], vm["cipher"])
	}

	hy2 := byName["hy2"]
	if hy2["sni"] != "c.com" || hy2["type"] != "hysteria2" {
		t.Errorf("hysteria2 应映射 sni, got=%v", hy2)
	}
	if _, bad := hy2["tls"]; bad {
		t.Errorf("hysteria2 不应有 tls 字段")
	}
	if _, bad := hy2["udp"]; bad {
		t.Errorf("hysteria2 不应有 udp 字段")
	}

	tu := byName["tu"]
	if tu["type"] != "tuic" || tu["uuid"] != "53fac7ed-2b9e-43f6-ab96-9d37d4667f94" {
		t.Errorf("tuic 映射错误: %v", tu)
	}
	if _, bad := tu["tls"]; bad {
		t.Errorf("tuic 不应有 tls 字段")
	}
}
