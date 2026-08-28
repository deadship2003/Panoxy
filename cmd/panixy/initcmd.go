package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/asset"
	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/locker"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/statemode"
	"github.com/deadship2003/panixy/internal/subscribe"
	"github.com/deadship2003/panixy/internal/systemdunit"
	"github.com/deadship2003/panixy/internal/upgrade"
)

// runInit:不打包、单二进制裸机初始化 —— 自己下载全部资产并部署,随后导入订阅。
// 下载三级策略:直连(15s 硬顶)> 订阅引导代理(需本机有可用内核) > gh 镜像。
func runInit(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	total := 9
	stepf := func(i int, f string, a ...any) { logx.Step("[%d/%d] %s", i, total, fmt.Sprintf(f, a...)) }

	name, _ := cmd.Flags().GetString("name")
	file, _ := cmd.Flags().GetString("file")
	mode, _ := cmd.Flags().GetString("proxy-mode")
	secret, _ := cmd.Flags().GetString("secret")
	mirrors, _ := cmd.Flags().GetStringSlice("mirror")
	if err := subscribe.CheckName(name); err != nil {
		return err
	}
	var url string
	if len(args) > 0 {
		url = args[0]
	}

	stepf(1, "环境预检(root/systemd/架构/旧版残留)")
	if runtimeArch() == "" {
		return fmt.Errorf("不支持的架构(仅 amd64/arm64)")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("需要 systemd")
	}
	if legacy := systemdunit.DetectLegacy(p); legacy != "" {
		return fmt.Errorf("检测到 bash 旧版部署残留:%s(先 sudo panixy uninstall 并删除 /etc/clash.yaml,详见 README 迁移一节)", legacy)
	}

	stepf(2, "获取订阅内容(%s)", orURL(url, file))
	var body []byte
	if file != "" {
		if body, err = subscribe.ValidateFile(file); err != nil {
			return err
		}
	} else {
		if url == "" {
			if isTTY() {
				fmt.Fprint(os.Stderr, "请粘贴订阅链接(整行粘贴后回车,无需加引号): ")
			}
			line, _ := readLine()
			url = line
			if url == "" {
				return fmt.Errorf("用法: panixy init [订阅URL] [--file 本地订阅文件]")
			}
		}
		if err := subscribe.CheckURL(url); err != nil {
			return err
		}
		api := mihomoapi.NewFromConf(p.Conf)
		if body, err = fetchSubBody(url, api); err != nil {
			return fmt.Errorf("订阅拉取失败:%v(订阅必须直连可达;或任意设备下载后用 --file 离线导入)", err)
		}
	}

	stepf(3, "网络探测:直连 GitHub 发布资产(15s 硬顶)")
	mirrorList := mirrors
	// 探测必须打真实资产 URL:CN 环境常见 github.com 主页通、
	// releases 资产域(objects.githubusercontent)被墙
	probeURL := "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
	allowDirect := directAssetReachable(probeURL, 15*time.Second)
	if allowDirect {
		logx.Info("直连可用,直接下载")
	} else {
		logx.Info("直连不可用(资产域不可达),下载将走订阅引导代理/镜像")
	}
	// 惰性引导代理:首个需要它的下载发生时才启动(用已取得的订阅体)
	proxyOnce := false
	proxyFn := func() string {
		if !proxyOnce {
			proxyOnce = true
			bootProxyFromSub(body, cmd) // 设置 bootProxyAddr()
		}
		return bootProxyAddr()
	}

	tmp, _ := os.MkdirTemp("", "panixy-init-")
	defer os.RemoveAll(tmp)

	stepf(4, "下载 mihomo 内核(%s)", runtimeArch())
	kernel := ""
	for _, base := range upgrade.CoreAssetCandidates("v1.19.30") {
		kurl := fmt.Sprintf("https://github.com/MetaCubeX/mihomo/releases/download/v1.19.30/%s.gz", base)
		if dlAny(kurl, allowDirect, proxyFn, mirrorList, filepath.Join(tmp, "core.gz"), "内核 "+shortAsset(base)) {
			core := filepath.Join(tmp, "core")
			if err := upgrade.GunzipFile(filepath.Join(tmp, "core.gz"), core); err != nil {
				continue
			}
			os.Chmod(core, 0o755)
			if err := upgrade.VerifyCore(core, ""); err != nil {
				logx.Step("%v,降级下一档", err)
				continue
			}
			kernel = core
			break
		}
	}
	if kernel == "" {
		return fmt.Errorf("内核下载失败(直连/代理/镜像均不可得)")
	}

	stepf(5, "下载 geo 数据与广告规则")
	geoBase := "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
	geos := map[string]string{
		"GeoIP.dat":    geoBase + "/geoip.dat",
		"GeoSite.dat":  geoBase + "/geosite.dat",
		"Country.mmdb": geoBase + "/country.mmdb",
	}
	geoOK := 0
	for f, u := range geos {
		if dlAny(u, allowDirect, proxyFn, mirrorList, filepath.Join(tmp, f), "geo "+f) {
			geoOK++
		}
	}
	ruleURL := "https://raw.githubusercontent.com/TG-Twilight/AWAvenue-Ads-Rule/refs/heads/main/Filters/AWAvenue-Ads-Rule-Clash-Classical.yaml"
	haveRule := dlAny(ruleURL, allowDirect, proxyFn, mirrorList, filepath.Join(tmp, "AWAvenue-Ads.yaml"), "广告规则")
	if geoOK < 3 {
		return fmt.Errorf("geo 数据下载不完整(%d/3)", geoOK)
	}

	stepf(6, "下载 metacubexd 面板")
	uiOK := dlAny("https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz",
		allowDirect, proxyFn, mirrorList, filepath.Join(tmp, "ui.tgz"), "面板")

	// ---- 至此资产齐备,以下与 deploy 同构 ----
	snap := snapshot(p)
	stepf(7, "资产就位 /opt/panixy + 渲染配置(密钥 %s)", secret)
	os.MkdirAll(filepath.Join(p.Root, "bin"), 0o755)
	if err := copyFile(kernel, p.Bin); err != nil {
		return err
	}
	os.Chmod(p.Bin, 0o755)
	if err := checkBinary(p); err != nil {
		return err
	}
	for f := range geos {
		copyFile(filepath.Join(tmp, f), filepath.Join(p.Root, f))
	}
	os.MkdirAll(p.RuleProv, 0o755)
	if haveRule {
		copyFile(filepath.Join(tmp, "AWAvenue-Ads.yaml"), filepath.Join(p.RuleProv, "AWAvenue-Ads.yaml"))
	}
	if uiOK {
		os.MkdirAll(p.UiDir, 0o755)
		if out, err := runTarX(tmp, "ui.tgz", p.UiDir); err != nil {
			logx.Warn("面板解包失败(%s),跳过面板", firstLineOf(out))
		} else {
			os.WriteFile(p.UiStamp, []byte("unknown\n"), 0o644)
		}
	}
	confNew := false
	if _, err := os.Stat(p.Conf); err == nil {
		logx.Info("检测到现有配置,保留不动:%s(分组规则与自定义参数全部继承)", p.Conf)
	} else {
		d := asset.DefaultConfigData()
		d.TProxy = mode == "tproxy"
		d.Secret = secret
		out, err := asset.RenderConfig(d)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p.Conf, []byte(out), 0o644); err != nil {
			return err
		}
		confNew = true
	}
	_ = confNew
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)
	if self != p.Cli {
		os.MkdirAll(filepath.Dir(p.Cli), 0o755)
		if b, err := os.ReadFile(self); err == nil {
			os.WriteFile(p.Cli, b, 0o755)
		}
	}
	installMan(p.ManGz, self)
	statemode.Write(p.State, statemode.State{ProxyMode: mode})

	stepf(8, "部署服务(单元/防火墙/健康验证)")
	if err := runInstall(cmd, args); err != nil {
		deployRollback(p, snap)
		return err
	}

	stepf(9, "导入订阅(%s)", name)
	subFile := filepath.Join(tmp, "sub.yaml")
	os.WriteFile(subFile, body, 0o644)
	setCmd := &cobra.Command{}
	setCmd.Flags().String("name", name, "")
	setCmd.Flags().String("file", subFile, "")
	setCmd.Flags().StringSlice("group", nil, "")
	if err := runSetSub(setCmd, []string{url}); err != nil {
		return fmt.Errorf("订阅导入失败:%v(资产与服务已就绪,可稍后 sudo panixy set-sub 重试)", err)
	}
	if bp := bootProxyAddr(); bp != "" {
		bootProxyStop()
		logx.Info("引导代理已清理")
	}
	logx.Info("init 完成:panixy status 查看健康;面板 http://<本机IP>:%d/ui/(密钥 %s)", constants.ApiPortDef, secret)
	return nil
}

// ---- 辅助 ----

func orURL(u, f string) string {
	if f != "" {
		return "本地文件 " + f
	}
	if u != "" {
		return u
	}
	return "粘贴模式"
}

func shortAsset(base string) string {
	if len(base) > 28 {
		return base[:28] + "…"
	}
	return base
}

// directAssetReachable 用 Range 探测真实发布资产(只取 1 字节,15s 硬顶)。
func directAssetReachable(u string, timeout time.Duration) bool {
	hc := &http.Client{Timeout: timeout}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode < 400
}

// dlAny 依次尝试:直连(仅当探测通过,免重复烧 15s)→ 订阅代理(惰性启动)→ 镜像前缀。
func dlAny(url string, allowDirect bool, proxyFn func() string, mirrors []string, dst, label string) bool {
	if allowDirect {
		if err := upgrade.DownloadProgress(url, "", dst, label); err == nil {
			return true
		}
	}
	if p := proxyFn(); p != "" {
		if err := upgrade.DownloadProgress(url, p, dst, label); err == nil {
			return true
		}
	}
	for _, m := range mirrors {
		m = strings.TrimRight(m, "/") + "/" + url
		if err := upgrade.DownloadProgress(m, "", dst, label+"(镜像)"); err == nil {
			logx.Info("%s 经镜像下载成功(镜像属第三方源;内核已做试运行校验)", label)
			return true
		}
	}
	return false
}

// bootProxyAddr / bootProxyStop / bootProxyFromSub 订阅引导代理(包级状态)。
var bootProxyDir, bootProxyPort = "", ""

func bootProxyAddr() string {
	if bootProxyDir == "" {
		return ""
	}
	return "http://127.0.0.1:" + bootProxyPort
}

func bootProxyStop() {
	if bootProxyDir == "" {
		return
	}
	if b, err := os.ReadFile(filepath.Join(bootProxyDir, "pid")); err == nil {
		for _, l := range strings.Fields(string(b)) {
			syscallKill(l)
		}
	}
	os.RemoveAll(bootProxyDir)
	bootProxyDir = ""
}

func syscallKill(pid string) {
	var p int
	if n, _ := fmt.Sscanf(pid, "%d", &p); n == 1 && p > 1 {
		syscall.Kill(p, syscall.SIGTERM)
	}
}

// bootProxyFromSub 用已取得的订阅体起临时 mihomo 引导代理(需本机有内核)。
func bootProxyFromSub(body []byte, cmd *cobra.Command) string {
	bootBin, _ := cmd.Flags().GetString("boot-bin")
	if bootBin == "" {
		bootBin = "/opt/panixy/bin/mihomo"
	}
	if _, err := os.Stat(bootBin); err != nil {
		logx.Step("无引导内核(%s),跳过订阅代理路径", bootBin)
		return ""
	}
	port := freePortStr()
	dir, _ := os.MkdirTemp("", "panixy-boot-")
	conf := fmt.Sprintf(`mixed-port: %s
mode: rule
log-level: warning
proxy-providers:
  boot:
    type: file
    path: ./boot.sub.yaml
    health-check: {enable: false}
proxy-groups:
  - {name: P, type: select, use: [boot]}
rules:
  - MATCH,P
`, port)
	os.WriteFile(filepath.Join(dir, "boot.yaml"), []byte(conf), 0o644)
	os.WriteFile(filepath.Join(dir, "boot.sub.yaml"), body, 0o644)
	c := exec.Command(bootBin, "-f", "boot.yaml", "-d", dir)
	c.Dir = dir
	if err := c.Start(); err != nil {
		os.RemoveAll(dir)
		return ""
	}
	os.WriteFile(filepath.Join(dir, "pid"), []byte(fmt.Sprint(c.Process.Pid)), 0o644)
	bootProxyDir, bootProxyPort = dir, port
	// 等就绪(15s)
	for i := 0; i < 15; i++ {
		hc := &http.Client{Timeout: 3 * time.Second}
		pu, _ := url.Parse("http://127.0.0.1:" + port)
		hc.Transport = &http.Transport{Proxy: http.ProxyURL(pu)}
		req, _ := http.NewRequest("GET", "https://www.gstatic.com/generate_204", nil)
		if resp, err := hc.Do(req); err == nil {
			resp.Body.Close()
			logx.Info("订阅引导代理已就绪(127.0.0.1:%s,临时内核 pid %d)", port, c.Process.Pid)
			return "http://127.0.0.1:" + port
		}
		time.Sleep(time.Second)
	}
	bootProxyStop()
	return ""
}

func freePortStr() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "33999"
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

func runTarX(dir, tgz, dst string) (string, error) {
	os.MkdirAll(dst, 0o755)
	out, err := exec.Command("tar", "xzf", filepath.Join(dir, tgz), "-C", dst).CombinedOutput()
	return string(out), err
}

// fetchSubBody 订阅拉取:直连优先,失败经本机 mixed-port 代理(升级订阅场景)。
func fetchSubBody(u string, api *mihomoapi.Client) ([]byte, error) {
	proxy := ""
	if api.Mixed > 0 {
		proxy = fmt.Sprintf("http://127.0.0.1:%d", api.Mixed)
	}
	var buf bytes.Buffer
	if err := subscribe.Fetch(u, proxy, subscribe.UA(""), &buf); err != nil {
		return nil, err
	}
	if err := subscribe.Validate(buf.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
