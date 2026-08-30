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

	"github.com/deadship2003/Panoxy/internal/asset"
	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/mihomoapi"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/statemode"
	"github.com/deadship2003/Panoxy/internal/subscribe"
	"github.com/deadship2003/Panoxy/internal/systemdunit"
	"github.com/deadship2003/Panoxy/internal/upgrade"
)

// runInit: no packaging, single-binary bare-metal init — downloads all assets and deploys by itself, then imports the subscription.
// Three-tier download strategy: direct (15s hard cap) > subscription bootstrap proxy (needs a usable kernel on this machine) > gh mirror.
func runInit(cmd *cobra.Command, args []string) error {
	if dry, _ := cmd.Flags().GetBool("dry-run"); dry {
		return initDryRun(cmd, args)
	}
	return withRootLock(func(p paths.Paths) error { return runInitBody(p, cmd, args) })
}

func runInitBody(p paths.Paths, cmd *cobra.Command, args []string) error {
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
	var err error
	if len(args) > 0 {
		url = args[0]
	}

	stepf(1, "environment precheck (root/systemd/arch/legacy residue)")
	if runtimeArch() == "" {
		return fmt.Errorf("unsupported architecture (only amd64/arm64)")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemd required")
	}
	if legacy := systemdunit.DetectLegacy(p); legacy != "" {
		return fmt.Errorf("bash legacy deployment residue detected: %s (first sudo %s uninstall and delete %s, see the README migration section)", legacy, constants.ProgName, constants.DefConfPath)
	}

	stepf(2, "fetch subscription content (%s)", orURL(url, file))
	var body []byte
	if file != "" {
		if body, err = subscribe.ValidateFile(file); err != nil {
			return err
		}
	} else {
		if url == "" {
			if url, err = promptSubURL(constants.ProgName + " init [subscription-URL] [--file local-subscription-file]"); err != nil {
				return err
			}
		}
		if err := subscribe.CheckURL(url); err != nil {
			return err
		}
		api := mihomoapi.NewFromConf(p.Conf)
		if body, err = fetchSubBody(url, api); err != nil {
			return fmt.Errorf("subscription fetch failed: %v (the subscription must be directly reachable; or download it on any device and import offline with --file)", err)
		}
	}

	stepf(3, "network probe: direct GitHub release assets (15s hard cap)")
	mirrorList := mirrors
	// The probe must hit a real asset URL: in CN environments the github.com homepage often works while
	// the releases asset domain (objects.githubusercontent) is blocked.
	probeURL := "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
	allowDirect := directAssetReachable(probeURL, 15*time.Second)
	if allowDirect {
		logx.Info("direct connection works, downloading directly")
	} else {
		logx.Info("direct connection unavailable (asset domain unreachable), downloads will go through the subscription bootstrap proxy / mirror")
	}
	// Lazy bootstrap proxy: only starts when the first download that needs it happens (using the fetched subscription body).
	proxyOnce := false
	proxyFn := func() string {
		if !proxyOnce {
			proxyOnce = true
			bootProxyFromSub(body, cmd) // sets bootProxyAddr()
		}
		return bootProxyAddr()
	}

	tmp, _ := os.MkdirTemp("", "panixy-init-")
	defer os.RemoveAll(tmp)

	stepf(4, "download mihomo kernel (%s)", runtimeArch())
	coreVer, err := detectCoreVer(proxyFn)
	if err != nil {
		return fmt.Errorf("cannot probe the latest mihomo kernel version: %v (use the offline package sudo ./%s deploy, or manually copy the kernel to %s and chmod +x then retry)", err, constants.ProgName, p.Bin)
	}
	logx.Info("latest upstream kernel detected at runtime: %s", coreVer)
	kernel := ""
	for _, base := range upgrade.CoreAssetCandidates(coreVer) {
		kurl := fmt.Sprintf("https://github.com/MetaCubeX/mihomo/releases/download/%s/%s.gz", coreVer, base)
		if downloadAny(kurl, allowDirect, proxyFn, mirrorList, filepath.Join(tmp, "core.gz"), "kernel "+shortAsset(base)) {
			core := filepath.Join(tmp, "core")
			if err := upgrade.GunzipFile(filepath.Join(tmp, "core.gz"), core); err != nil {
				continue
			}
			os.Chmod(core, 0o755)
			if err := upgrade.VerifyCore(core, coreVer); err != nil {
				logx.Step("%v, degrading to next candidate", err)
				continue
			}
			kernel = core
			break
		}
	}
	if kernel == "" {
		return fmt.Errorf("kernel download failed (direct/proxy/mirror all unavailable); use the offline package sudo ./%s deploy, or manually copy the mihomo kernel to %s and chmod +x then retry", constants.ProgName, p.Bin)
	}

	stepf(5, "download geo data and ad rules")
	geoBase := "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
	geos := map[string]string{
		"GeoIP.dat":    geoBase + "/geoip.dat",
		"GeoSite.dat":  geoBase + "/geosite.dat",
		"Country.mmdb": geoBase + "/country.mmdb",
	}
	geoOK := 0
	for f, u := range geos {
		if downloadAny(u, allowDirect, proxyFn, mirrorList, filepath.Join(tmp, f), "geo "+f) {
			geoOK++
		}
	}
	ruleURL := "https://github.com/Lynricsy/HyperADRules/releases/latest/download/hyper_adrules_ads_clash.yaml"
	haveRule := downloadAny(ruleURL, allowDirect, proxyFn, mirrorList, filepath.Join(tmp, "HyperADRules-Ads.yaml"), "ad rules")
	if geoOK < 3 {
		return fmt.Errorf("geo data download incomplete (%d/3)", geoOK)
	}

	stepf(6, "download metacubexd web UI")
	uiOK := downloadAny("https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz",
		allowDirect, proxyFn, mirrorList, filepath.Join(tmp, "ui.tgz"), "web UI")

	// ---- assets are all ready now; from here on it is structurally identical to deploy ----
	snap := snapshot(p)
	stepf(7, "place assets in %s + render config (secret %s)", constants.DefRootDir, secret)
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
		copyFile(filepath.Join(tmp, "HyperADRules-Ads.yaml"), filepath.Join(p.RuleProv, "HyperADRules-Ads.yaml"))
	}
	if uiOK {
		os.MkdirAll(p.UiDir, 0o755)
		if err := runExtractTgz(filepath.Join(tmp, "ui.tgz"), p.UiDir); err != nil {
			logx.Warn("UI unpack failed (%s), skipping UI", firstLineOf(err.Error()))
		} else {
			os.WriteFile(p.UiStamp, []byte("unknown\n"), 0o644)
		}
	}
	confNew := false
	if _, err := os.Stat(p.Conf); err == nil {
		logx.Info("existing config detected, kept untouched: %s (group rules and custom parameters are all inherited)", p.Conf)
	} else {
		d := asset.DefaultConfigData()
		d.TProxy = mode == "tproxy"
		d.Secret = secret
		out, err := asset.RenderConfig(d)
		if err != nil {
			return err
		}
		// The clean default-template copy (for merge-conf to rebuild its baseline) and the actual config are written at the same time.
		if err := os.WriteFile(p.DefaultConf, []byte(out), 0o644); err != nil {
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

	stepf(8, "deploy service (units/firewall/health verification)")
	if err := runInstall(cmd, args); err != nil {
		deployRollback(p, snap)
		return err
	}

	stepf(9, "import subscription (%s)", name)
	subFile := filepath.Join(tmp, "sub.yaml")
	os.WriteFile(subFile, body, 0o644)
	setCmd := &cobra.Command{}
	setCmd.Flags().String("name", name, "")
	setCmd.Flags().String("file", subFile, "")
	setCmd.Flags().StringSlice("group", nil, "")
	if err := runSubImport(setCmd, []string{url}); err != nil {
		return fmt.Errorf("subscription import failed: %v (assets and service are ready, you can retry later with sudo %s sub import)", err, constants.ProgName)
	}
	if bp := bootProxyAddr(); bp != "" {
		bootProxyStop()
		logx.Info("bootstrap proxy cleaned up")
	}
	logx.Info("init complete: %s status to check health; web UI http://<host-IP>:%d/ui/ (secret %s)", constants.ProgName, constants.ApiPortDef, secret)
	return nil
}

// ---- helpers ----

func orURL(u, f string) string {
	if f != "" {
		return "local file " + f
	}
	if u != "" {
		return u
	}
	return "paste mode"
}

func shortAsset(base string) string {
	if len(base) > 28 {
		return base[:28] + "…"
	}
	return base
}

// detectCoreVer probes the latest upstream kernel version at runtime (never hard-coded): direct → subscription bootstrap proxy.
// The direct probe does not depend on allowDirect (api.github.com and the release asset domain are different domains; one may work
// while the other does not); only on failure does it borrow a subscription node to start a bootstrap proxy and retry; if both fail
// it returns an error and falls back to the offline package / a manually placed kernel.
func detectCoreVer(proxyFn func() string) (string, error) {
	if v, err := upgrade.Latest("MetaCubeX/mihomo", ""); err == nil {
		return v, nil
	}
	if p := proxyFn(); p != "" {
		if v, err := upgrade.Latest("MetaCubeX/mihomo", p); err == nil {
			return v, nil
		}
	}
	return "", fmt.Errorf("GitHub API unreachable (both direct and subscription proxy failed)")
}

// directAssetReachable probes a real release asset with a Range request (fetch 1 byte only, 15s hard cap).
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

// downloadAny tries in order: direct (only when the probe passed, to avoid burning another 15s) → subscription proxy (lazy start) → mirror prefixes.
func downloadAny(url string, allowDirect bool, proxyFn func() string, mirrors []string, dst, label string) bool {
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
		if err := upgrade.DownloadProgress(m, "", dst, label+"(mirror)"); err == nil {
			logx.Info("%s downloaded via mirror (mirror is a third-party source; the kernel was verified by a trial run)", label)
			return true
		}
	}
	return false
}

// bootProxyAddr / bootProxyStop / bootProxyFromSub subscription bootstrap proxy (package-level state).
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

// bootProxyFromSub starts a temporary mihomo bootstrap proxy using the fetched subscription body (needs a kernel on this machine).
func bootProxyFromSub(body []byte, cmd *cobra.Command) string {
	bootBin, _ := cmd.Flags().GetString("boot-bin")
	if bootBin == "" {
		bootBin = paths.Get().Bin // follows --root/env; falls back to default /opt when nothing is installed
	}
	if _, err := os.Stat(bootBin); err != nil {
		// Bare-metal with no kernel: cannot start a bootstrap proxy via a subscription node. Print clear guidance then skip, falling back to mirror/offline package.
		logx.Step("no bootstrap kernel (%s), cannot download assets via a subscription node", bootBin)
		logx.Step("  option 1 (recommended): run make package on a machine with internet to build an offline package → then sudo ./%s deploy on the target", constants.ProgName)
		logx.Step("  option 2: manually copy the mihomo kernel to %s and chmod +x, then rerun init", bootBin)
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
	// The bootstrap kernel likewise only accepts Clash YAML: normalize non-Clash formats (sing-box/Surge/base64-Clash) first,
	// otherwise the bootstrap proxy won't start and all subsequent asset downloads will fail.
	bootBody, _, err := subscribe.Normalize(body)
	if err != nil {
		bootBody = body // fall back to the original on normalization failure, letting mihomo report its own error (consistent with the real import path)
	}
	os.WriteFile(filepath.Join(dir, "boot.sub.yaml"), bootBody, 0o644)
	c := exec.Command(bootBin, "-f", "boot.yaml", "-d", dir)
	c.Dir = dir
	if err := c.Start(); err != nil {
		os.RemoveAll(dir)
		return ""
	}
	os.WriteFile(filepath.Join(dir, "pid"), []byte(fmt.Sprint(c.Process.Pid)), 0o644)
	bootProxyDir, bootProxyPort = dir, port
	// Wait for readiness (15s).
	for i := 0; i < 15; i++ {
		hc := &http.Client{Timeout: 3 * time.Second}
		pu, _ := url.Parse("http://127.0.0.1:" + port)
		hc.Transport = &http.Transport{Proxy: http.ProxyURL(pu)}
		req, _ := http.NewRequest("GET", "https://www.gstatic.com/generate_204", nil)
		if resp, err := hc.Do(req); err == nil {
			resp.Body.Close()
			logx.Info("subscription bootstrap proxy ready (127.0.0.1:%s, temp kernel pid %d)", port, c.Process.Pid)
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

// fetchSubBody fetches a subscription: direct first, falling back to the local mixed-port proxy (upgrade-subscription scenario).
func fetchSubBody(u string, api *mihomoapi.Client) ([]byte, error) {
	var buf bytes.Buffer
	if err := subscribe.Fetch(u, api.Proxy(), subscribe.UA(), &buf); err != nil {
		return nil, err
	}
	if err := subscribe.Validate(buf.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// initDryRun is a dry-run: environment checks + download strategy decision + placement list + config render preview.
// No downloads, no writes, no root needed; for a full sandbox run use panixy try.
func initDryRun(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	logx.Info("== init --dry-run (dry-run mode, does not execute) ==")
	logx.Info("target dir: %s (changeable via --root; config stays system-level %s)", p.Root, p.Conf)
	logx.Info("CLI/man: %s / %s", p.Cli, p.ManGz)

	logx.Step("[precheck] environment")
	arch := runtimeArch()
	if arch == "" {
		return fmt.Errorf("unsupported architecture (only amd64/arm64)")
	}
	logx.Info("  arch %s ✓  systemd:%s", arch, orOK(exists("/usr/bin/systemctl") || exists("/bin/systemctl")))
	if legacy := systemdunit.DetectLegacy(p); legacy != "" {
		logx.Warn("  ⚠️ bash legacy residue detected: %s (a real install would be aborted, clean up first)", legacy)
	} else {
		logx.Info("  legacy residue: none ✓")
	}
	if _, err := os.Stat(p.Conf); err == nil {
		logx.Info("  existing config: present, will be inherited as-is (groups/custom parameters untouched)")
	} else {
		logx.Info("  existing config: none, will render the default template (secret %s, ports 33833/9966/6699/9999)", drySecret(cmd))
	}

	logx.Step("[precheck] download strategy (probe the real asset domain, 15s hard cap)")
	probe := "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
	if directAssetReachable(probe, 15*time.Second) {
		logx.Info("  direct works → download directly")
	} else {
		logx.Info("  direct unavailable → subscription bootstrap proxy (needs a kernel on this machine: %s)%s",
			orOK(exists(bootDefaultBin(cmd))), " → mirror (--mirror)")
	}

	logx.Step("[plan] download list")
	logx.Info("  kernel: mihomo-linux-%s-<latest upstream auto-detected at runtime>.gz (candidate degradation v3→standard→compatible)", arch)
	logx.Info("  geo:  GeoIP.dat / GeoSite.dat / Country.mmdb")
	logx.Info("  rules: HyperADRules-Ads.yaml   web UI: metacubexd compressed-dist.tgz")

	logx.Step("[plan] config render preview (stdout)")
	d := asset.DefaultConfigData()
	d.TProxy = modeOf(cmd) == "tproxy"
	d.Secret = drySecret(cmd)
	out, err := asset.RenderConfig(d)
	if err != nil {
		return err
	}
	fmt.Print(out)
	logx.Info("== dry-run done. Real run: sudo %s init ...; full sandbox run: %s try ...", constants.ProgName, constants.ProgName)
	return nil
}

func drySecret(cmd *cobra.Command) string {
	s, _ := cmd.Flags().GetString("secret")
	return s
}
func modeOf(cmd *cobra.Command) string {
	m, _ := cmd.Flags().GetString("proxy-mode")
	return m
}
func bootDefaultBin(cmd *cobra.Command) string {
	b, _ := cmd.Flags().GetString("boot-bin")
	if b == "" {
		b = paths.Get().Bin
	}
	return b
}
func orOK(ok bool) string {
	if ok {
		return "available ✓"
	}
	return "missing"
}
