package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/config"
	"github.com/deadship2003/panixy/internal/execx"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/locker"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/mihomoapi"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/subscribe"
	"github.com/deadship2003/panixy/internal/systemdunit"
)

func needRoot() error {
	if os.Getenv("PANIXY_ALLOW_NONROOT") != "" {
		return nil // 测试沙箱钩子:e2e 用,生产勿设
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("请用 sudo 运行")
	}
	return nil
}

// withRootLock 统一「root 校验 + 进程锁」样板:校验通过后把路径交给 fn,返回时自动解锁。
// 进程内重入由 locker 支持,deploy→install、init→set-sub 等嵌套调用天然安全。
func withRootLock(fn func(p paths.Paths) error) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	return fn(p)
}

// mihomoTest 用内核 -t 校验配置(CombinedOutput:内核日志走 stdout,必须合并捕获)。
func mihomoTest(p paths.Paths, conf string) (string, error) {
	return execx.Run(p.Bin, "-t", "-f", conf, "-d", p.Root)
}

// runSetSub 实现 set-sub:预取→校验→增量编辑→-t→预置缓存→重启→验证节点数。
// 任何一步失败恢复备份(带缓存),绝不假成功。
func runSetSub(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error { return runSetSubBody(p, cmd, args) })
}

func runSetSubBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	file, _ := cmd.Flags().GetString("file")
	groups, _ := cmd.Flags().GetStringSlice("group")
	if err := subscribe.CheckName(name); err != nil {
		return err
	}

	var url string
	var err error
	if len(args) > 0 {
		url = args[0]
	} else {
		if url, err = promptSubURL("panixy set-sub [订阅URL] [--file 本地文件](或无参数进入粘贴模式)"); err != nil {
			return err
		}
	}
	if err := subscribe.CheckURL(url); err != nil {
		return err
	}

	// 1) 取订阅内容:本地文件 > 直连 > 经本机代理
	var body []byte
	if file != "" {
		if body, err = subscribe.ValidateFile(file); err != nil {
			return err
		}
		logx.Info("使用本地订阅文件: %s(跳过联网拉取)", file)
	} else {
		api := mihomoapi.NewFromConf(p.Conf)
		var buf bytes.Buffer
		if err := subscribe.Fetch(url, api.Proxy(), subscribe.UA(), &buf); err != nil {
			return fmt.Errorf(`订阅拉取失败(直连与经本机代理均不通): %v
  提示:命令行传 URL 须整体加单引号(含 & ? 等字符会被 shell 拆掉),或直接
  sudo panixy set-sub 回车进入粘贴模式;无外网环境可离线导入(任意设备下载好订阅后
  sudo panixy set-sub --file <订阅文件>),或指定可用代理 PANIXY_PROXY`, err)
		}
		if err := subscribe.Validate(buf.Bytes()); err != nil {
			return err
		}
		body = buf.Bytes()
	}

	// 归一化:sing-box/Surge/base64-Clash 等非 Clash YAML 格式统一转成 Clash YAML;
	// Clash YAML / URI 列表 mihomo 原生解析,原样透传(converted 决定 provider 是否切 file)。
	nb, conv, err := subscribe.Normalize(body)
	if err != nil {
		return err
	}
	body, converted := nb, conv
	if converted {
		logx.Step("订阅非 Clash YAML 格式,已归一化;provider 切换为本地缓存模式(type: file)")
	}

	// 2) 备份 → 增量编辑 → -t 校验 → 预置缓存
	// 汇总全部 provider 的节点名(含本次导入),供派生组剪枝:只保留实际命中的地区/类型组
	nodeNames := collectNodeNames(p, body, name)
	e, err := config.Load(p.Conf)
	if err != nil {
		return err
	}
	if err := config.Backup(p.Conf); err != nil {
		return fmt.Errorf("备份配置失败: %w", err)
	}
	cache := filepath.Join(p.Proxies, name+".yaml")
	if b, err := os.ReadFile(cache); err == nil {
		os.WriteFile(cache+".panixy-bak", b, 0o644)
	}
	recoverAll := func(restart bool) {
		config.Restore(p.Conf)
		if b, err := os.ReadFile(cache + ".panixy-bak"); err == nil {
			os.WriteFile(cache, b, 0o644)
		}
		os.Remove(cache + ".panixy-bak")
		if restart {
			systemdunit.Restart()
		}
	}
	// 全新系统:模板占位订阅(SUB_URL_PLACEHOLDER)在首个真实订阅导入时自动退场,
	// 避免留下一个永远拉不到的空 provider(继承自现有配置的真实条目不受影响)
	for _, pn := range e.Providers() {
		if u, ok := e.ProviderURL(pn); ok && u == "SUB_URL_PLACEHOLDER" && pn != name {
			e.RemoveProvider(pn)
			e.WireProvider(pn, false, nil)
			logx.Step("占位订阅 %s 已由真实订阅 %s 取代", pn, name)
		}
	}
	rel := "./proxies/" + name + ".yaml"
	if err := e.SetProvider(name, url, rel); err != nil {
		recoverAll(false)
		return err
	}
	if err := e.SetProviderType(name, converted); err != nil {
		recoverAll(false)
		return err
	}
	e.WireProvider(name, true, groups)
	if pruned := e.PruneDerived(nodeNames); pruned > 0 {
		logx.Info("按实际节点剔除 %d 个无匹配的地区/类型组(只保留有效分组)", pruned)
	}
	if err := e.Save(); err != nil {
		recoverAll(false)
		return fmt.Errorf("配置写入失败: %w", err)
	}
	if out, err := mihomoTest(p, p.Conf); err != nil {
		msg := firstErrLine(out)
		recoverAll(false)
		return fmt.Errorf("配置校验未通过(%s),已恢复原配置", msg)
	}
	if err := os.MkdirAll(p.Proxies, 0o755); err != nil {
		recoverAll(false)
		return err
	}
	if err := os.WriteFile(cache, body, 0o644); err != nil {
		recoverAll(false)
		return err
	}

	// 3) 重启重建 provider(热重载不刷新 provider —— mihomo 限制)+ 验证节点数
	logx.Step("订阅已写入 provider %s,缓存已预置,重启内核生效(换 URL 必须重启)", name)
	if err := systemdunit.Restart(); err != nil {
		recoverAll(false)
		return fmt.Errorf("重启失败,已恢复原订阅")
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		recoverAll(true)
		return fmt.Errorf("重启后健康检查超时,已恢复原订阅:%w", err)
	}
	api := mihomoapi.NewFromConf(p.Conf)
	nodes := 0
	for i := 0; i < 5; i++ {
		if st, err := api.Provider(name); err == nil {
			nodes = st.Nodes
			if nodes > 0 {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if nodes == 0 {
		recoverAll(true)
		return fmt.Errorf("订阅 %s 未加载(节点数为 0),已恢复原订阅;排查: panixy log / panixy check", name)
	}
	logx.Info("订阅(%s)加载成功:%d 个节点(测速选优由 🔃 自动选择 组负责,默认走最快节点)", name, nodes)
	config.ClearBackup(p.Conf)
	os.Remove(cache + ".panixy-bak")
	logx.Info("订阅导入完成: %s", url)
	return nil
}

func runSubRm(cmd *cobra.Command, args []string) error {
	return withRootLock(func(p paths.Paths) error { return runSubRmBody(p, cmd, args) })
}

func runSubRmBody(p paths.Paths, cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if err := subscribe.CheckName(name); err != nil {
		return err
	}
	e, err := config.Load(p.Conf)
	if err != nil {
		return err
	}
	if _, ok := e.ProviderURL(name); !ok {
		return fmt.Errorf("订阅 %s 不存在(现有: %v)", name, e.Providers())
	}
	if err := config.Backup(p.Conf); err != nil {
		return err
	}
	e.RemoveProvider(name)
	e.WireProvider(name, false, nil)
	if err := e.Save(); err != nil {
		config.Restore(p.Conf)
		return err
	}
	if out, err := mihomoTest(p, p.Conf); err != nil {
		msg := firstErrLine(out)
		config.Restore(p.Conf)
		return fmt.Errorf("删除后校验未通过(%s —— 删光唯一订阅会使组失去 use),已恢复", msg)
	}
	os.Remove(filepath.Join(p.Proxies, name+".yaml"))
	if err := systemdunit.Restart(); err != nil {
		config.Restore(p.Conf)
		systemdunit.Restart()
		return fmt.Errorf("重启失败,已恢复")
	}
	if err := health.WaitHealthy(p.Conf, 30*time.Second, ""); err != nil {
		config.Restore(p.Conf)
		systemdunit.Restart()
		return fmt.Errorf("重启后健康检查超时,已恢复:%w", err)
	}
	config.ClearBackup(p.Conf)
	logx.Info("订阅 %s 已删除并生效", name)
	return nil
}

func runSubList(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	asJSON, _ := cmd.Flags().GetBool("json")
	e, err := config.Load(p.Conf)
	if err != nil {
		return err
	}
	names := e.Providers()
	api := mihomoapi.NewFromConf(p.Conf)
	stats := make([]mihomoapi.ProviderStat, 0, len(names))
	for _, n := range names {
		st, _ := api.Provider(n) // 单订阅故障不影响其他展示
		if st.Name == "" {
			st.Name = n
		}
		stats = append(stats, st)
	}
	if asJSON {
		b, _ := json.Marshal(stats)
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("%-16s %-8s %-6s %s\n", "NAME", "STATE", "NODES", "ERROR")
	for _, st := range stats {
		state := "✅"
		if st.Error != "" || st.Nodes == 0 {
			state = "⚠️"
		}
		fmt.Printf("%-16s %-8s %-6d %s\n", st.Name, state, st.Nodes, st.Error)
	}
	return nil
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func readLine() (string, error) {
	r := bufio.NewReader(os.Stdin)
	s, err := r.ReadString('\n')
	return strings.TrimSpace(s), err
}

// promptSubURL 无 URL 参数时的粘贴模式:读整行(URL 含 & ? 无需加引号),空输入返回用法错误。
func promptSubURL(usage string) (string, error) {
	if isTTY() {
		fmt.Fprint(os.Stderr, "请粘贴订阅链接(整行粘贴后回车,无需加引号): ")
	}
	line, _ := readLine()
	if line == "" {
		return "", fmt.Errorf("用法: %s", usage)
	}
	return line, nil
}

// collectNodeNames 汇总全部 provider 的节点名(含本次导入 body),供派生组剪枝用。
// 必须覆盖所有订阅:某地区/类型组只要被任意订阅命中就保留,避免误删。
func collectNodeNames(p paths.Paths, newBody []byte, newName string) []string {
	seen := map[string]bool{}
	add := func(b []byte) {
		if ns, err := subscribe.NodeNames(b); err == nil {
			for _, n := range ns {
				seen[n] = true
			}
		}
	}
	add(newBody)
	if entries, err := os.ReadDir(p.Proxies); err == nil {
		for _, ent := range entries {
			if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".yaml") || ent.Name() == newName+".yaml" {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(p.Proxies, ent.Name())); err == nil {
				add(b)
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	return names
}

var errLineRe = regexp.MustCompile(`level=(error|fatal) msg="([^"]*)"`)

// firstErrLine 从 mihomo 输出提取首条错误(bash 时代教训:错误在 stdout)。
func firstErrLine(out string) string {
	if m := errLineRe.FindStringSubmatch(out); m != nil {
		return m[2]
	}
	if len(out) > 160 {
		return out[len(out)-160:]
	}
	return out
}
