// Package mihomoapi 封装 mihomo external-controller REST API。
// 注意(实测事实,写入注释防止误用):PUT /configs 热重载会重新 parse 并重建
// provider 对象,但不会重拉订阅内容(Initial 只读本地缓存、不碰远程 URL);
// 免重启重拉用 PUT /providers/proxies/{name},增删 provider 或重连分组仍须重启进程。
package mihomoapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/deadship2003/panoxy/internal/constants"

	"gopkg.in/yaml.v3"
)

type Client struct {
	Base   string // http://127.0.0.1:9999
	Secret string
	Mixed  int // mixed-port(本机代理跳板)
	hc     *http.Client
}

// NewFromConf 从 mihomo 配置文件解析 secret/external-controller/mixed-port,
// 环境变量 PANIXY_API/PANIXY_SECRET/PANIXY_PROXY_PORT 可覆盖(沙箱测试用)。
func NewFromConf(confPath string) *Client {
	c := &Client{hc: &http.Client{Timeout: 5 * time.Second}}
	var raw struct {
		Secret             string `yaml:"secret"`
		ExternalController string `yaml:"external-controller"`
		MixedPort          int    `yaml:"mixed-port"`
	}
	if b, err := os.ReadFile(confPath); err == nil {
		yaml.Unmarshal(b, &raw) // 解析失败则全走默认
	}
	c.Secret = raw.Secret
	if c.Secret == "" {
		c.Secret = constants.DefSecret
	}
	c.Mixed = raw.MixedPort
	api := "http://127.0.0.1:" + portOf(raw.ExternalController, constants.ApiPortDef)
	if p := os.Getenv(constants.EnvPrefix() + "_API_PORT"); p != "" {
		api = "http://127.0.0.1:" + p
	}
	if u := os.Getenv(constants.EnvPrefix() + "_API"); u != "" {
		api = u
	}
	if s := os.Getenv(constants.EnvPrefix() + "_SECRET"); s != "" {
		c.Secret = s
	}
	if p := os.Getenv(constants.EnvPrefix() + "_PROXY_PORT"); p != "" && c.Mixed == 0 {
		fmt.Sscanf(p, "%d", &c.Mixed)
	}
	c.Base = strings.TrimRight(api, "/")
	return c
}

func portOf(ctrl string, def int) string {
	if i := strings.LastIndex(ctrl, ":"); i >= 0 && i+1 < len(ctrl) {
		return ctrl[i+1:]
	}
	return fmt.Sprint(def)
}

// Proxy 返回本机 mixed-port 代理地址(未配置时为空串)。
func (c *Client) Proxy() string {
	if c.Mixed <= 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", c.Mixed)
}

// call 发送带密钥的 API 请求并返回响应体;状态码 ≥300 视为错误。
func (c *Client) call(method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return b, fmt.Errorf("API %s %s → HTTP %d", method, path, resp.StatusCode)
	}
	return b, nil
}

// Version 返回内核版本号(如 v1.19.30);不可达返回错误。
func (c *Client) Version() (string, error) {
	b, err := c.call("GET", "/version", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Version == "" {
		return "", fmt.Errorf("unexpected version response: %s", string(b))
	}
	return v.Version, nil
}

// ProviderStat 单个订阅的健康快照。
type ProviderStat struct {
	Name  string `json:"name"`
	Nodes int    `json:"nodes"`
	Type  string `json:"type"`
	Error string `json:"error,omitempty"`
}

type providerResp struct {
	Name        string `json:"name"`
	VehicleType string `json:"vehicleType"`
	Proxies     []struct {
		Name string `json:"name"`
	} `json:"proxies"`
}

// Provider 查询单个 provider 节点数(正确解码 JSON,而非 bash 时代的 grep 计数)。
func (c *Client) Provider(name string) (ProviderStat, error) {
	b, err := c.call("GET", "/providers/proxies/"+name, nil)
	var st ProviderStat
	st.Name = name
	if err != nil {
		st.Error = "fetch failed: " + err.Error()
		return st, err
	}
	var pr providerResp
	if err := json.Unmarshal(b, &pr); err != nil {
		st.Error = "parse failed: " + err.Error()
		return st, err
	}
	st.Nodes = len(pr.Proxies)
	st.Type = pr.VehicleType
	return st, nil
}

// ReloadConf 热重载配置:会重建 provider 对象但不重拉订阅内容,仅适用于不改动 provider 的变更。
func (c *Client) ReloadConf(path string) error {
	_, err := c.call("PUT", "/configs?force=0", map[string]string{"path": path})
	return err
}

// RawGet 探活类 GET:返回 HTTP 状态码字符串(不做 JSON 解码)。
func (c *Client) RawGet(path string) (string, error) {
	resp, err := c.hc.Get(c.Base + path)
	if err != nil {
		return "000", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return fmt.Sprintf("%d", resp.StatusCode), nil
}
