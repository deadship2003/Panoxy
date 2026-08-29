// Package subscribe 订阅预取与校验(直连优先,失败走本机 mixed-port 代理跳板)。
package subscribe

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/deadship2003/panixy/internal/logx"
)

// UA 返回拉取订阅的 User-Agent。
// 实测同一机场对不同 UA 返回不同节点数:ClashMetaForAndroid/clash-verge 拿到
// 最多(44 个),clash.meta 只有 41 个。取最大值的 UA,固定返回。
func UA() string {
	return "ClashMetaForAndroid/2.11.5"
}

// Fetch 拉取订阅到 w:直连优先,失败经本机 mixed-port 代理(换被墙订阅时旧节点当跳板)。
// proxy 形如 http://127.0.0.1:33833,空则只试直连。
func Fetch(url, proxy, ua string, w io.Writer) error {
	try := func(p string) error {
		tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // 订阅常见自签/IP 直连
		hc := &http.Client{Timeout: 20 * time.Second, Transport: tr}
		if p != "" {
			tr.Proxy = http.ProxyURL(mustURL(p))
		}
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", ua)
		resp, err := hc.Do(req)
		if err != nil {
			logx.Debug("订阅拉取(%s)失败: %v", orDefault(p, "直连"), err)
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		_, err = io.Copy(w, resp.Body)
		return err
	}
	if err := try(""); err == nil {
		return nil
	} else {
		logx.Step("订阅直连拉取失败,改走本机代理: %v", err)
	}
	if proxy == "" {
		return fmt.Errorf("直连失败且无本机代理可用")
	}
	return try(proxy)
}

// Validate 校验订阅内容:必须能识别出格式且至少含一个节点。
// 覆盖所有标准订阅格式(Clash YAML / URI 列表 / sing-box / Surge),见 normalize.go;
// 机场对无效 token 常返回网页/空,必须拦下(bash 时代实测教训)。
func Validate(b []byte) error {
	if len(strings.TrimSpace(string(b))) == 0 {
		return fmt.Errorf("订阅内容为空")
	}
	if Detect(b) == FormatUnknown {
		return fmt.Errorf("订阅不是可识别的格式(支持 Clash YAML / base64或明文 URI 列表 / sing-box JSON / Surge;机场对无效 token 常返回网页)")
	}
	if nodeCount(b) == 0 {
		return fmt.Errorf("订阅中未解析到任何节点(检查链接/token;机场对无效请求可能返回网页)")
	}
	return nil
}

// ValidateFile 校验本地订阅文件并读回内容。
func ValidateFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取本地订阅文件失败: %w", err)
	}
	if err := Validate(b); err != nil {
		return nil, err
	}
	return b, nil
}

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// CheckName 校验 provider 名称(防注入 YAML 键与 API 路径)。
func CheckName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("名称只能包含 [a-zA-Z0-9_-]:%q", name)
	}
	return nil
}

// CheckURL 校验订阅 URL。
func CheckURL(u string) error {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("URL 需以 http(s):// 开头(命令行传参记得整体加单引号,或直接 panixy set-sub 回车粘贴)")
	}
	return nil
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }
func orDefault(s, d string) string {
	if s != "" {
		return s
	}
	return d
}
