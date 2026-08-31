// Package upgrade 面向 GitHub 的内核/面板升级:查最新、下载(经本机代理优先)、
// 试运行校验、原子替换、备份轮换。编排(重启+健康+回滚)在命令层。
package upgrade

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/deadship2003/Panoxy/internal/httpx"
	"github.com/deadship2003/Panoxy/internal/logx"
)

// Latest 查询 repo(如 MetaCubeX/mihomo)最新稳定 tag;经本机代理优先,失败直连。
func Latest(repo, proxy string) (string, error) {
	api := "https://api.github.com/repos/" + repo + "/releases/latest"
	fetch := func(p string) (string, error) {
		hc := httpx.Client(p, 15*time.Second)
		resp, err := hc.Get(api)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil || rel.TagName == "" {
			return "", fmt.Errorf("unexpected release response")
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
		hc := httpx.Client(p, 300*time.Second)
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
	logx.Step("download via proxy failed, retrying direct: %s", urlStr)
	return try("")
}

// DownloadProgress 带进度条下载:统一 10 分钟超时(Content-Length 已知时渲染百分比)。
// 连通性探测(15s 硬顶)已由调用方 directAssetReachable 完成,此处不做重复判定;
// 大文件下载正常约需 10-30s,15s 硬顶会误杀大文件下载(实测教训)。
func DownloadProgress(urlStr, proxy, dst, label string) error {
	return downloadOnce(urlStr, proxy, dst, label, 600*time.Second)
}

func downloadOnce(urlStr, proxy, dst, label string, timeout time.Duration) error {
	hc := httpx.Client(proxy, timeout)
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
