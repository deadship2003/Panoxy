// Package upgrade 面向 GitHub 的内核/面板升级:查最新、下载(经本机代理优先)、
// 试运行校验、原子替换、备份轮换。编排(重启+健康+回滚)在命令层。
package upgrade

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/deadship2003/panixy/internal/logx"
)

// Latest 查询 repo(如 MetaCubeX/mihomo)最新稳定 tag;经本机代理优先,失败直连。
func Latest(repo, proxy string) (string, error) {
	api := "https://api.github.com/repos/" + repo + "/releases/latest"
	fetch := func(p string) (string, error) {
		hc := &http.Client{Timeout: 15 * time.Second}
		if p != "" {
			u, _ := url.Parse(p)
			hc.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
		resp, err := hc.Get(api)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil || rel.TagName == "" {
			return "", fmt.Errorf("release 响应异常")
		}
		return rel.TagName, nil
	}
	if v, err := fetch(proxy); err == nil {
		return v, nil
	}
	return fetch("")
}

// Download 下载 url 到 dst(经代理优先,失败直连;>=300 视为失败)。
func Download(urlStr, proxy, dst string) error {
	try := func(p string) error {
		hc := &http.Client{Timeout: 300 * time.Second}
		if p != "" {
			u, _ := url.Parse(p)
			hc.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
		resp, err := hc.Get(urlStr)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, resp.Body)
		return err
	}
	if err := try(proxy); err == nil {
		return nil
	}
	logx.Step("经代理下载失败,改直连: %s", urlStr)
	return try("")
}

var verRe = regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+`)

// VerifyCore 试运行解包后的内核并核对版本(空/损坏内核会被 shell 当空脚本执行
// 而假通过 —— bash 时代实测教训,必须校验输出内容)。
func VerifyCore(bin, wantVer string) error {
	out, err := exec.Command(bin, "-v").CombinedOutput()
	logx.DebugCmd(bin, []string{"-v"}, string(out), err)
	if err != nil || !strings.Contains(string(out), "Mihomo") {
		return fmt.Errorf("新内核无法运行(指令集不兼容?)")
	}
	got := verRe.FindString(string(out))
	if wantVer != "" && got != wantVer {
		return fmt.Errorf("版本不符:期望 %s 得到 %s", wantVer, got)
	}
	return nil
}

// GunzipFile 解压 .gz 文件到目标。
func GunzipFile(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("压缩包损坏: %w", err)
	}
	defer zr.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, zr)
	return err
}

// CoreAssetCandidates 按架构/AVX2 给出候选资产基名(依次降级)。
func CoreAssetCandidates(ver string) []string {
	base := "mihomo-linux-" + runtime.GOARCH
	if runtime.GOARCH == "amd64" && hasAVX2() {
		return []string{base + "-v3-" + ver, base + "-" + ver, base + "-compatible-" + ver}
	}
	return []string{base + "-" + ver, base + "-compatible-" + ver}
}

func hasAVX2() bool {
	b, err := os.ReadFile("/proc/cpuinfo")
	return err == nil && strings.Contains(string(b), "avx2")
}

// DownloadProgress 带进度条下载:统一 10 分钟超时(Content-Length 已知时渲染百分比)。
// 连通性探测(15s 硬顶)已由调用方 directAssetReachable 完成,此处不做重复判定;
// 18MB 内核正常下载约需 10-30s,15s 硬顶会误杀大文件下载(实测教训)。
func DownloadProgress(urlStr, proxy, dst, label string) error {
	return downloadOnce(urlStr, proxy, dst, label, 600*time.Second)
}

func downloadOnce(urlStr, proxy, dst, label string, timeout time.Duration) error {
	hc := &http.Client{Timeout: timeout}
	if proxy != "" {
		u, _ := url.Parse(proxy)
		hc.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
	}
	req, _ := http.NewRequest("GET", urlStr, nil)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	pg := logx.NewProgress(label, resp.ContentLength)
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	var n int64
	buf := make([]byte, 64<<10)
loop:
	for {
		m, rerr := resp.Body.Read(buf)
		if m > 0 {
			if _, werr := f.Write(buf[:m]); werr != nil {
				f.Close()
				pg.Done(werr)
				return werr
			}
			n += int64(m)
			pg.Update(n)
		}
		if rerr != nil {
			if rerr == io.EOF {
				break loop
			}
			f.Close()
			pg.Done(rerr)
			return rerr
		}
	}
	err = f.Close()
	pg.Done(err)
	return err
}
