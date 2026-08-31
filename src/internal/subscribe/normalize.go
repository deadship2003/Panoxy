// 订阅格式识别与归一化:把任意标准订阅格式统一成 mihomo 能解析的 Clash YAML。
//
// 关键事实(经 mihomo v1.19.30 实测):
//   - mihomo 的 proxy-provider(无论 type: http / file)原生只解析 Clash YAML,
//     以及 base64/明文 URI 列表(vless/vmess/trojan/ss/ssr/hysteria2/tuic ...)。
//   - mihomo 不能原生解析:sing-box JSON、Surge 配置、base64 编码的 Clash YAML。
//     这三类必须由 Panoxy 在缓存前归一化成 Clash YAML,并把 provider 切成 type: file
//     (否则内核重启刷新时会重新拉原始 URL 再解析失败)。
//
// 因此这里的职责不是为某个机场写专用解析,而是覆盖所有标准订阅格式的通用识别与转换。
package subscribe

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Format 订阅内容格式。
type Format int

const (
	FormatUnknown     Format = iota // 无法识别(空、HTML 错误页等)
	FormatClash                     // Clash YAML(proxies: 列表)
	FormatURI                       // 明文 URI 列表(每行一个 scheme://...)
	FormatBase64URI                 // base64 编码的 URI 列表(mihomo 可原生解码)
	FormatBase64Clash               // base64 编码的 Clash YAML(需解码)
	FormatSingBox                   // sing-box JSON(outbounds:)
	FormatSurge                     // Surge 配置(#!MANAGED-CONFIG / [Proxy])
)

// uriScheme 常见的代理 URI scheme(用于判定一行是否为节点 URI)。
var uriSchemeRe = regexp.MustCompile(`^(vless|vmess|trojan|ss|ssr|hysteria2?|hy2|tuic|snell|wireguard|http|https|socks5)://`)

// Detect 识别订阅内容格式。
func Detect(b []byte) Format {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return FormatUnknown
	}
	// Surge 托管配置头
	if strings.HasPrefix(s, "#!MANAGED-CONFIG") {
		return FormatSurge
	}
	// sing-box JSON
	if strings.HasPrefix(s, "{") {
		var j map[string]any
		if json.Unmarshal([]byte(s), &j) == nil {
			if _, ok := j["outbounds"]; ok {
				return FormatSingBox
			}
		}
	}
	// Clash YAML(含 proxies 键)
	var doc map[string]any
	if yaml.Unmarshal([]byte(s), &doc) == nil {
		if _, ok := doc["proxies"]; ok {
			return FormatClash
		}
	}
	// Surge 无托管头的纯配置([Proxy] 段)
	if strings.Contains(s, "[Proxy]") {
		return FormatSurge
	}
	// 明文 URI 列表
	if isURILine(firstNonEmptyLine(s)) {
		return FormatURI
	}
	// base64:解码后可能是 URI 列表或 Clash YAML
	if dec, err := decodeBase64Line(s); err == nil {
		d := strings.TrimSpace(dec)
		if d == "" {
			return FormatUnknown
		}
		if isURILine(firstNonEmptyLine(d)) {
			return FormatBase64URI
		}
		var doc2 map[string]any
		if yaml.Unmarshal([]byte(d), &doc2) == nil {
			if _, ok := doc2["proxies"]; ok {
				return FormatBase64Clash
			}
		}
	}
	return FormatUnknown
}

// Normalize 把订阅内容归一化为 mihomo 能解析的 Clash YAML。
// 返回归一化后的字节、是否发生转换(需要 provider 切 type: file)、以及错误。
// Clash YAML / URI 列表(明文或 base64)mihomo 原生解析,原样透传(converted=false)。
func Normalize(b []byte) ([]byte, bool, error) {
	switch Detect(b) {
	case FormatBase64Clash:
		dec, err := decodeBase64Line(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, false, fmt.Errorf("failed to decode base64 Clash: %w", err)
		}
		return []byte(dec), true, nil
	case FormatSingBox:
		out, err := singboxToClash(b)
		return out, true, err
	case FormatSurge:
		out, err := surgeToClash(b)
		return out, true, err
	case FormatUnknown:
		return nil, false, fmt.Errorf("subscription is not in a recognizable format (supported: Clash YAML / URI list / sing-box JSON / Surge; airports often return a web page for an invalid token)")
	default:
		return b, false, nil
	}
}

// NodeNames 提取订阅中的节点名(供派生组剪枝:只保留实际命中的地区/类型分组)。
// 覆盖全部标准格式;URI 列表取 #fragment 名称。
func NodeNames(b []byte) ([]string, error) {
	switch Detect(b) {
	case FormatClash:
		return clashNodeNames(b)
	case FormatURI:
		return uriNodeNames(b)
	case FormatBase64URI, FormatBase64Clash:
		dec, err := decodeBase64Line(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, err
		}
		if Detect([]byte(dec)) == FormatClash {
			return clashNodeNames([]byte(dec))
		}
		return uriNodeNames([]byte(dec))
	case FormatSingBox:
		return singboxNodeNames(b)
	case FormatSurge:
		return surgeNodeNames(b)
	default:
		return nil, fmt.Errorf("unrecognized subscription format (not Clash YAML / URI list / sing-box / Surge)")
	}
}

// ---- 各格式节点名提取 ----

func clashNodeNames(b []byte) ([]string, error) {
	var doc struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse Clash YAML: %w", err)
	}
	out := make([]string, 0, len(doc.Proxies))
	for _, p := range doc.Proxies {
		if p.Name != "" {
			out = append(out, p.Name)
		}
	}
	return out, nil
}

func uriNodeNames(b []byte) ([]string, error) {
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if n := uriFragmentName(line); n != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

func singboxNodeNames(b []byte) ([]string, error) {
	var doc struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse sing-box JSON: %w", err)
	}
	out := make([]string, 0, len(doc.Outbounds))
	for _, ob := range doc.Outbounds {
		if ob.Tag != "" {
			out = append(out, ob.Tag)
		}
	}
	return out, nil
}

// surgeProxyLines 返回 Surge 配置 [Proxy] 段内的非注释行(节点名提取与节点转换共用)。
func surgeProxyLines(b []byte) []string {
	var out []string
	inProxy := false
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			inProxy = strings.EqualFold(strings.Trim(t, "[]"), "Proxy")
			continue
		}
		if !inProxy || t == "" || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, t)
	}
	return out
}

func surgeNodeNames(b []byte) ([]string, error) {
	var out []string
	for _, line := range surgeProxyLines(b) {
		if i := strings.Index(line, "="); i > 0 {
			out = append(out, strings.TrimSpace(line[:i]))
		}
	}
	return out, nil
}

// ---- 转换:sing-box JSON → Clash YAML ----

func singboxToClash(b []byte) ([]byte, error) {
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse sing-box JSON: %w", err)
	}
	proxies := make([]map[string]any, 0, len(doc.Outbounds))
	for _, ob := range doc.Outbounds {
		if p, ok := singboxOutbound(ob); ok {
			proxies = append(proxies, p)
		}
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no convertible outbounds in sing-box JSON (only vless/vmess/trojan/shadowsocks/hysteria2/tuic supported)")
	}
	return renderClashProxies(proxies)
}

// singboxOutbound 把单个 sing-box outbound 映射为 Clash proxy(不支持的类型返回 ok=false 跳过)。
// 注意 TLS 字段名按协议不同:vless/vmess 用 tls+servername,trojan/hysteria2/tuic 用 sni;
// hysteria2/tuic 无 udp 字段(hysteria2 天生 UDP,tuic 用 udp-relay-mode)。
func singboxOutbound(ob map[string]any) (map[string]any, bool) {
	tag, _ := ob["tag"].(string)
	sbType, _ := ob["type"].(string)
	server, _ := ob["server"].(string)
	port := intVal(ob["server_port"])
	if server == "" || port == 0 {
		return nil, false // direct/dns-out 等非节点出站
	}
	p := map[string]any{"name": tag, "server": server, "port": port}

	var tlsEnabled bool
	var sni string
	var insecure bool
	if tls, ok := ob["tls"].(map[string]any); ok {
		tlsEnabled, _ = tls["enabled"].(bool)
		sni, _ = tls["server_name"].(string)
		insecure, _ = tls["insecure"].(bool)
	}

	switch sbType {
	case "vless":
		p["type"] = "vless"
		p["udp"] = true
		if u, _ := ob["uuid"].(string); u != "" {
			p["uuid"] = u
		}
		if f, _ := ob["flow"].(string); f != "" {
			p["flow"] = f
		}
		if tlsEnabled {
			p["tls"] = true
			if sni != "" {
				p["servername"] = sni
			}
		}
		if insecure {
			p["skip-cert-verify"] = true
		}
	case "vmess":
		p["type"] = "vmess"
		p["udp"] = true
		if u, _ := ob["uuid"].(string); u != "" {
			p["uuid"] = u
		}
		// mihomo vmess 要求显式给 alterId 与 cipher(缺省值也须写出,否则拒载)
		p["alterId"] = intVal(ob["alter_id"])
		cipher := "auto"
		if c, _ := ob["security"].(string); c != "" && c != "auto" {
			cipher = c
		}
		p["cipher"] = cipher
		if tlsEnabled {
			p["tls"] = true
			if sni != "" {
				p["servername"] = sni
			}
		}
		if insecure {
			p["skip-cert-verify"] = true
		}
	case "trojan":
		p["type"] = "trojan"
		p["udp"] = true
		if pw, _ := ob["password"].(string); pw != "" {
			p["password"] = pw
		}
		if sni != "" {
			p["sni"] = sni
		}
		if insecure {
			p["skip-cert-verify"] = true
		}
	case "shadowsocks":
		p["type"] = "ss"
		p["udp"] = true
		if m, _ := ob["method"].(string); m != "" {
			p["cipher"] = m
		}
		if pw, _ := ob["password"].(string); pw != "" {
			p["password"] = pw
		}
		if pf, _ := ob["plugin"].(string); pf != "" {
			p["plugin"] = pf
		}
	case "hysteria2":
		p["type"] = "hysteria2"
		if pw, _ := ob["password"].(string); pw != "" {
			p["password"] = pw
		}
		if sni != "" {
			p["sni"] = sni
		}
		if insecure {
			p["skip-cert-verify"] = true
		}
	case "tuic":
		p["type"] = "tuic"
		if u, _ := ob["uuid"].(string); u != "" {
			p["uuid"] = u
		}
		if pw, _ := ob["password"].(string); pw != "" {
			p["password"] = pw
		}
		if sni != "" {
			p["sni"] = sni
		}
		if insecure {
			p["skip-cert-verify"] = true
		}
	default:
		return nil, false
	}

	// 传输层仅 TCP 型协议有(ws/http/grpc)
	if tr, ok := ob["transport"].(map[string]any); ok {
		applySingboxTransport(p, tr)
	}
	return p, true
}

// applySingboxTransport 把 sing-box transport 映射为 Clash 的 network + 对应 opts。
func applySingboxTransport(p map[string]any, tr map[string]any) {
	t, _ := tr["type"].(string)
	switch t {
	case "ws":
		p["network"] = "ws"
		opts := map[string]any{}
		if path, _ := tr["path"].(string); path != "" {
			opts["path"] = path
		}
		if h, ok := tr["headers"].(map[string]any); ok && len(h) > 0 {
			hd := make(map[string]string, len(h))
			for k, v := range h {
				hd[k] = fmt.Sprint(v)
			}
			opts["headers"] = hd
		}
		if len(opts) > 0 {
			p["ws-opts"] = opts
		}
	case "http":
		p["network"] = "http"
		opts := map[string]any{}
		if path, _ := tr["path"].(string); path != "" {
			opts["path"] = []string{path}
		}
		if len(opts) > 0 {
			p["http-opts"] = opts
		}
	case "grpc":
		p["network"] = "grpc"
		if svc, _ := tr["service_name"].(string); svc != "" {
			p["grpc-opts"] = map[string]any{"grpc-service-name": svc}
		}
	}
}

// ---- 转换:Surge → Clash YAML ----

func surgeToClash(b []byte) ([]byte, error) {
	var proxies []map[string]any
	for _, line := range surgeProxyLines(b) {
		if p, ok := surgeProxy(line); ok {
			proxies = append(proxies, p)
		}
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no convertible nodes in Surge config [Proxy] section (only ss/trojan/vmess/vless/hysteria2 supported)")
	}
	return renderClashProxies(proxies)
}

// surgeProxy 解析一行 Surge 节点定义(SS/SSR/trojan/vmess/vless/hysteria2 等常见协议)。
func surgeProxy(line string) (map[string]any, bool) {
	i := strings.Index(line, "=")
	if i <= 0 {
		return nil, false
	}
	name := strings.TrimSpace(line[:i])
	parts := strings.Split(strings.TrimSpace(line[i+1:]), ",")
	if len(parts) < 3 {
		return nil, false
	}
	proto := strings.ToLower(strings.TrimSpace(parts[0]))
	server := strings.TrimSpace(parts[1])
	port := atoi(parts[2])
	if server == "" || port == 0 {
		return nil, false
	}
	params := map[string]string{}
	for _, kv := range parts[3:] {
		if j := strings.Index(kv, "="); j > 0 {
			params[strings.TrimSpace(kv[:j])] = strings.TrimSpace(kv[j+1:])
		}
	}

	p := map[string]any{"name": name, "server": server, "port": port}
	switch proto {
	case "ss", "shadowsocks":
		p["type"] = "ss"
		p["udp"] = true
		if m := params["encrypt-method"]; m != "" {
			p["cipher"] = m
		}
		if pw := params["password"]; pw != "" {
			p["password"] = pw
		}
	case "trojan":
		p["type"] = "trojan"
		p["udp"] = true
		if pw := params["password"]; pw != "" {
			p["password"] = pw
		}
		if sni := params["sni"]; sni != "" {
			p["sni"] = sni
		}
		if params["skip-cert-verify"] == "true" {
			p["skip-cert-verify"] = true
		}
	case "vmess":
		p["type"] = "vmess"
		p["udp"] = true
		if u := params["username"]; u != "" {
			p["uuid"] = u
		}
		// mihomo vmess 要求显式给 alterId 与 cipher(Surge 无 alterId,固定 0)
		p["alterId"] = 0
		cipher := "auto"
		if m := params["encrypt-method"]; m != "" {
			cipher = m
		}
		p["cipher"] = cipher
		applySurgeTLS(p, params)
		applySurgeWS(p, params)
	case "vless":
		p["type"] = "vless"
		p["udp"] = true
		if u := params["username"]; u != "" {
			p["uuid"] = u
		}
		applySurgeTLS(p, params)
		applySurgeWS(p, params)
	case "hysteria2", "hy2":
		p["type"] = "hysteria2"
		if pw := params["password"]; pw != "" {
			p["password"] = pw
		}
		if sni := params["sni"]; sni != "" {
			p["sni"] = sni
		}
		if params["skip-cert-verify"] == "true" {
			p["skip-cert-verify"] = true
		}
	default:
		return nil, false
	}
	return p, true
}

func applySurgeTLS(p map[string]any, params map[string]string) {
	if params["tls"] == "true" {
		p["tls"] = true
		if sni := params["sni"]; sni != "" {
			p["servername"] = sni
		}
		if params["skip-cert-verify"] == "true" {
			p["skip-cert-verify"] = true
		}
	}
}

func applySurgeWS(p map[string]any, params map[string]string) {
	if params["ws"] != "true" {
		return
	}
	p["network"] = "ws"
	opts := map[string]any{}
	if pth := params["ws-path"]; pth != "" {
		opts["path"] = pth
	}
	if h := params["ws-headers"]; h != "" {
		opts["headers"] = map[string]string{"Host": h}
	}
	if len(opts) > 0 {
		p["ws-opts"] = opts
	}
}

// ---- 通用工具 ----

// nodeCount 统计订阅中的节点数(识别格式后;供 Validate 判断"是否有节点")。
// 不要求节点有名:URI 列表按行计数,其余按条目计数,避免 URI 列表缺 #name 时被误判为 0。
func nodeCount(b []byte) int {
	return nodeCountDetected(b, Detect(b))
}

// nodeCountDetected 在已知格式下计数,避免 Validate 与 nodeCount 各自再做一次 Detect(重复解析)。
func nodeCountDetected(b []byte, f Format) int {
	switch f {
	case FormatClash:
		var doc struct {
			Proxies []map[string]any `yaml:"proxies"`
		}
		if yaml.Unmarshal(b, &doc) == nil {
			return len(doc.Proxies)
		}
	case FormatURI:
		return countURILines(string(b))
	case FormatBase64URI, FormatBase64Clash:
		if dec, err := decodeBase64Line(strings.TrimSpace(string(b))); err == nil {
			return nodeCount([]byte(dec)) // 解码后是新内容,重新识别
		}
	case FormatSingBox:
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if json.Unmarshal(b, &doc) == nil {
			return len(doc.Outbounds)
		}
	case FormatSurge:
		n := 0
		for _, line := range surgeProxyLines(b) {
			if strings.Contains(line, "=") {
				n++
			}
		}
		return n
	}
	return 0
}

func countURILines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if isURILine(l) {
			n++
		}
	}
	return n
}

// renderClashProxies 把 proxy 映射列表渲染为 Clash YAML(proxies: 段)。
func renderClashProxies(proxies []map[string]any) ([]byte, error) {
	out, err := yaml.Marshal(map[string]any{"proxies": proxies})
	if err != nil {
		return nil, fmt.Errorf("failed to render Clash YAML: %w", err)
	}
	return out, nil
}

// uriFragmentName 提取 URI 的 #fragment(节点名,百分号解码)。
func uriFragmentName(uri string) string {
	i := strings.IndexByte(uri, '#')
	if i < 0 || i+1 >= len(uri) {
		return ""
	}
	frag := uri[i+1:]
	if n, err := url.PathUnescape(frag); err == nil {
		return n
	}
	return frag
}

func isURILine(s string) bool {
	return uriSchemeRe.MatchString(strings.TrimSpace(s))
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// decodeBase64Line 解码订阅 base64(去空白,兼容标准/URL 编码与无填充)。
func decodeBase64Line(s string) (string, error) {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if dec, err := enc.DecodeString(clean); err == nil {
			return string(dec), nil
		}
	}
	return "", fmt.Errorf("base64 decode failed")
}

func intVal(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
