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
	if os.Geteuid() != 0 {
		return fmt.Errorf("请用 sudo 运行")
	}
	return nil
}

// mihomoTest 用内核 -t 校验配置(CombinedOutput:内核日志走 stdout,必须合并捕获)。
func mihomoTest(p paths.Paths, conf string) (string, error) {
	return execx.Run(p.Bin, "-t", "-f", conf, "-d", p.Root)
}

// runSetSub 实现 set-sub:预取→校验→增量编辑→-t→预置缓存→重启→验证节点数。
// 任何一步失败恢复备份(带缓存),绝不假成功。
func runSetSub(cmd *cobra.Command, args []string) error {
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()

	name, _ := cmd.Flags().GetString("name")
	file, _ := cmd.Flags().GetString("file")
	groups, _ := cmd.Flags().GetStringSlice("group")
	if err := subscribe.CheckName(name); err != nil {
		return err
	}

	var url string
	if len(args) > 0 {
		url = args[0]
	} else {
		// 粘贴模式:读整行,不经 shell 解析(URL 含 & ? 无需引号)
		if isTTY() {
			fmt.Fprint(os.Stderr, "请粘贴订阅链接(整行粘贴后回车,无需加引号): ")
		}
		line, _ := readLine()
		url = line
		if url == "" {
			return fmt.Errorf("用法: panixy set-sub [订阅URL] [--file 本地文件](或无参数进入粘贴模式)")
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
		proxy := ""
		if api.Mixed > 0 {
			proxy = fmt.Sprintf("http://127.0.0.1:%d", api.Mixed)
		}
		var buf bytes.Buffer
		if err := subscribe.Fetch(url, proxy, subscribe.UA(p.Bin), &buf); err != nil {
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

	// 2) 备份 → 增量编辑 → -t 校验 → 预置缓存
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
	rel := "./proxies/" + name + ".yaml"
	if err := e.SetProvider(name, url, rel); err != nil {
		recoverAll(false)
		return err
	}
	e.WireProvider(name, true, groups)
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
	if err := needRoot(); err != nil {
		return err
	}
	p := paths.Get()
	lk, err := locker.Lock(p.Lock)
	if err != nil {
		return err
	}
	defer lk.Unlock()
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
