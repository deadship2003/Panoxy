// Package httpx 统一「可选代理」的 HTTP 客户端构造:subscribe 与 upgrade 的多处
// 直连/代理回退都复刻同一段 Transport 装配,收敛到此避免漂移。
package httpx

import (
	"net/http"
	"net/url"
	"time"
)

// Transport 返回出站 Transport;proxy 非空则经该代理(空串=直连)。
func Transport(proxy string) *http.Transport {
	tr := &http.Transport{}
	if proxy != "" {
		u, _ := url.Parse(proxy)
		tr.Proxy = http.ProxyURL(u)
	}
	return tr
}

// Client 返回带超时的 http.Client;proxy 非空则经该代理出网。
func Client(proxy string, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport(proxy)}
}
