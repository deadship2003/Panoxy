// Package health 健康检测五要素:服务状态、API、各 provider 节点数、出口连通、防火墙残留。
// 核心教训(bash 时代实测):内核拉不到订阅时 API 照常应答 —— 只查 API 会假成功,
// 节点数才是"真的能转发"的前提。
package health

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/deadship2003/Panoxy/internal/config"
	"github.com/deadship2003/Panoxy/internal/core"
	"github.com/deadship2003/Panoxy/internal/firewall"
	"github.com/deadship2003/Panoxy/internal/mihomoapi"
	"github.com/deadship2003/Panoxy/internal/statemode"
	"github.com/deadship2003/Panoxy/internal/systemdunit"
)

type Report struct {
	Service   string                   `json:"service"`
	APIAlive  bool                     `json:"api"`
	APIVer    string                   `json:"api_version,omitempty"`
	FwBackend string                   `json:"firewall"`
	Stale     bool                     `json:"stale_rules"`
	Mode      string                   `json:"mode"`
	Providers []mihomoapi.ProviderStat `json:"providers"`
	Nodes     int                      `json:"nodes"`        // 全部 provider 节点合计
	Egress    string                   `json:"proxy_egress"` // 经 mixed-port 访问 generate_204 的状态码
	Direct    string                   `json:"direct_egress"`
	CoreVer   string                   `json:"core,omitempty"`
	UIVer     string                   `json:"ui,omitempty"`
	LastUp    string                   `json:"last_upgrade,omitempty"`
}

// Collect 收集健康快照。confPath 用于构造 API 客户端与 provider 名单。
// 单项失败不影响其他项(探测永不致命)。
func Collect(confPath, uiStamp, lastUp, statePath string) Report {
	r := Report{Service: systemdunit.Active(), Mode: modeOf(statePath)}
	api := mihomoapi.NewFromConf(confPath)
	if v, err := api.Version(); err == nil {
		r.APIAlive = true
		r.APIVer = v
	}
	r.FwBackend = firewall.BackendName
	r.Stale, _ = firewall.HasStaleRules()
	if e, err := config.Load(confPath); err == nil {
		for _, name := range e.Providers() {
			st, _ := api.Provider(name)
			r.Providers = append(r.Providers, st)
			r.Nodes += st.Nodes
		}
	}
	r.Egress = probe204(true, api.Mixed)
	r.Direct = probe204(false, 0)
	r.CoreVer = core.Version()
	if b, err := os.ReadFile(uiStamp); err == nil {
		r.UIVer = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(lastUp); err == nil {
		r.LastUp = strings.TrimSpace(string(b))
	}
	return r
}

func modeOf(statePath string) string {
	return statemode.Read(statePath)
}

func probe204(viaProxy bool, port int) string {
	target := "http://connect.rom.miui.com/generate_204"
	if viaProxy {
		target = "https://www.gstatic.com/generate_204"
	}
	req, _ := http.NewRequest("GET", target, nil)
	hc := &http.Client{Timeout: 8 * time.Second}
	if viaProxy && port > 0 {
		u, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		hc.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "000"
	}
	resp.Body.Close()
	return fmt.Sprintf("%d", resp.StatusCode)
}

// EgressOK 经代理出网 204,重试 retries 次(升级健康检查用)。
func EgressOK(port int, retries int) bool {
	for i := 0; i < retries; i++ {
		if code := probe204(true, port); code == "204" {
			return true
		}
		time.Sleep(3 * time.Second)
	}
	return false
}

// WaitHealthy 轮询等待服务+API 就绪(带超时,代替固定 sleep;慢网关不误判)。
// expectVer 非空时还要求 API 上报该版本。
func WaitHealthy(confPath string, timeout time.Duration, expectVer string) error {
	api := mihomoapi.NewFromConf(confPath)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		v, err := api.Version()
		if err == nil && systemdunit.IsActive() &&
			(expectVer == "" || v == expectVer) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("health check timed out (service/API not ready)")
}
