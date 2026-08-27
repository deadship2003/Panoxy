// panixy — 基于 mihomo 的 Linux 透明代理网关部署/管理工具(Go 版单二进制)。
// 职责边界:模板渲染、文件/单元管理、防火墙 DNS 劫持、订阅管理、升级回滚、健康检测;
// 业务转发与 DNS 解析全部由 mihomo 内核完成。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/firewall"
	"github.com/deadship2003/panixy/internal/logx"
	"github.com/deadship2003/panixy/internal/paths"
	"github.com/deadship2003/panixy/internal/statemode"
)

func main() {
	// cobra 不自带 -?:入口处归一化
	for i, a := range os.Args {
		if a == "-?" {
			os.Args[i] = "--help"
		}
	}
	if err := NewRootCmd().Execute(); err != nil {
		logx.Error("%v", err)
		os.Exit(1)
	}
}

// notImplemented 是 P0 阶段的占位:命令注册与帮助先行,实现按阶段交付。
func notImplemented(name string) error {
	return fmt.Errorf("%s 尚未实现(按开发计划在后续阶段交付,当前为 P0 骨架)", name)
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "panixy",
		Short: "基于 mihomo 的透明代理网关部署/管理工具",
		Long: `panixy — 基于 mihomo(TUN/TPROXY)的 Linux 透明代理网关部署/管理工具

数据面(节点/策略组选择)在 Web 面板;传输面(tun/tproxy 模式、防火墙)在本 CLI。

引导(在解压的离线包根目录运行):
  sudo ./panixy deploy '订阅链接'        # 全新部署,可顺带导入订阅

日常管理:
  sudo panixy set-sub '订阅链接'          # 回车进入粘贴模式,URL 无需加引号
  panixy status                          # 健康一览(节点/服务/防火墙/出网)
  sudo panixy upgrade --check            # 查看可升级项

详细说明: man panixy(部署后可用)或 panixy man`,
		Version: constants.Version,
	}
	root.PersistentFlags().Bool("verbose", false, "分步明细:每个事务步骤、写入的文件、应用的规则")
	root.PersistentFlags().Bool("debug", false, "全量透传:外部命令原样回显、mihomo API 请求响应、配置 diff")
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if d, _ := cmd.Flags().GetBool("debug"); d {
			logx.SetLevel(logx.LevelDebug)
		} else if v, _ := cmd.Flags().GetBool("verbose"); v {
			logx.SetLevel(logx.LevelVerbose)
		}
	}
	root.AddCommand(
		cmdDeploy(), cmdInstall(), cmdSetSub(), cmdSubRm(), cmdSubList(),
		cmdStatus(), cmdMode(), cmdUpgrade(), cmdUpdateUI(), cmdRollback(),
		cmdUninstall(), cmdUnits(), cmdLog(), cmdCheck(), cmdApplyConf(),
		cmdFw(), cmdMan(),
	)
	return root
}

func cmdDeploy() *cobra.Command {
	c := &cobra.Command{
		Use:   "deploy [订阅URL]",
		Short: "全新部署(离线包内运行):资产就位+服务拉起+可选订阅导入",
		Long: `全新部署,须在解压的离线包根目录运行。

流程:放置内核/geo/面板/广告规则 → 渲染配置(现有 > 包内手工 > 模板)→
安装 CLI 与 man 手册 → 写入 systemd 单元 → 开启 ip_forward → 拉起服务(含防火墙)。
任一步失败全量回滚。检测到 bash 旧版部署残留(unit 含 resolvectl/配置含 dns-hijack)时中止并给出手动清理指引。

示例:
  sudo ./panixy deploy 'https://example.com/sub?token=x&sid=y'   # 部署并导入订阅
  sudo ./panixy deploy --proxy-mode tproxy                        # 以 TPROXY 模式部署`,
		RunE: func(cmd *cobra.Command, args []string) error { return notImplemented("deploy") },
	}
	c.Flags().String("name", "SUB", "订阅 provider 名称(仅 [a-zA-Z0-9_-])")
	c.Flags().String("file", "", "本地订阅 YAML 文件(无外网时离线导入)")
	c.Flags().String("proxy-mode", "tun", "透明代理模式: tun | tproxy")
	return c
}

func cmdInstall() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "仅部署 systemd 服务与防火墙(文件已就位时;deploy 的内部步骤)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("install") },
	}
}

func cmdSetSub() *cobra.Command {
	c := &cobra.Command{
		Use:   "set-sub [订阅URL]",
		Short: "导入/更换订阅:预取→预置缓存→重启→验证节点数>0",
		Long: `导入或更换订阅。无参数时进入粘贴模式(读整行,URL 含 & ? 等字符无需引号)。

流程:预拉取(本地文件 > 直连 > 经本机代理)→ 校验为含节点的 Clash YAML →
以 yaml 增量编辑写入 proxy-providers[NAME](复用锚点 <<: *p,保留其他 provider 与注释,
并把 NAME 融合进各组 use 列表)→ mihomo -t 校验 → 预置 provider 缓存 → 重启
(热重载不刷新 provider,mihomo 限制)→ 查询该 provider 节点数,=0 自动回滚。

前置要求:配置中存在锚点 &p(基础模板自带)。`,
		Example: "  sudo panixy set-sub --name airport2 'https://example.com/sub2'\n  sudo panixy set-sub   # 粘贴模式",
		RunE:    func(cmd *cobra.Command, args []string) error { return notImplemented("set-sub") },
	}
	c.Flags().String("name", "SUB", "订阅 provider 名称(默认 SUB;仅 [a-zA-Z0-9_-])")
	c.Flags().String("file", "", "本地订阅 YAML 文件(跳过联网拉取)")
	c.Flags().StringSlice("group", nil, "限定融合的组(默认:全部 use 非空的组/锚点持有者)")
	return c
}

func cmdSubRm() *cobra.Command {
	c := &cobra.Command{
		Use:   "sub-rm --name NAME",
		Short: "删除指定订阅 provider(备份、校验、重启;失败回滚)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("sub-rm") },
	}
	c.Flags().String("name", "", "要删除的 provider 名称(必填)")
	_ = c.MarkFlagRequired("name")
	return c
}

func cmdSubList() *cobra.Command {
	c := &cobra.Command{
		Use:   "sub-list",
		Short: "列出全部订阅:状态/节点数/错误(单订阅故障不影响其他展示)",
		Long: `读取配置全部 proxy-providers,逐个调用 mihomo API 查询状态。

状态:✅正常 / ⚠️获取失败 / ⚠️解析失败 / ⚠️节点为0。--json 输出机器可读格式。`,
		RunE: func(cmd *cobra.Command, args []string) error { return notImplemented("sub-list") },
	}
	c.Flags().Bool("json", false, "以 JSON 输出")
	return c
}

func cmdStatus() *cobra.Command {
	c := &cobra.Command{
		Use:   "status [-v|-q|--json]",
		Short: "健康一览:服务/防火墙/各订阅节点数/内核UI版本/出口连通",
		Long: `健康一览。包含:服务状态、防火墙后端与残留规则、全部 proxy-providers 状态、
内核/UI 版本、上次升级时间、代理出口连通性;并提示浏览器 DoH 无法被内核劫持。

  -v  追加明细:当前代理模式(tun/tproxy)、TUN 栈风险提示、路由/缓存细节
  -q  静默,仅退出码:0健康 1降级(节点0或代理出网不通) 2故障(服务/API 不可用)
  --json 机器可读单行`,
		RunE: func(cmd *cobra.Command, args []string) error { return notImplemented("status") },
	}
	c.Flags().BoolP("verbose", "v", false, "追加明细")
	c.Flags().BoolP("quiet", "q", false, "静默,仅退出码")
	c.Flags().Bool("json", false, "以 JSON 输出")
	return c
}

func cmdMode() *cobra.Command {
	return &cobra.Command{
		Use:   "mode [tun|tproxy]",
		Short: "查看或切换透明代理模式(原子切换:防火墙+配置+重启)",
		Long: `查看或切换 tun/tproxy 模式。

切换是原子事务:卸载旧防火墙规则 → 渲染对应配置变体 → 重启服务 → 加载新防火墙 →
健康检查,任一步失败整体回滚。TPROXY 前置依赖内核 xt_TPROXY 模块,不可用时拒绝切换。

注意:模式无法在 Web 面板切换 —— 防火墙规则与配置必须同事务变更,面板只管数据面(节点/组)。
不带参数则显示当前模式。`,
		RunE: func(cmd *cobra.Command, args []string) error { return notImplemented("mode") },
	}
}

func cmdUpgrade() *cobra.Command {
	c := &cobra.Command{
		Use:   "upgrade [--core|--ui] [--core-version vX] [--ui-version vX] [--check]",
		Short: "升级内核与/或面板(timer 每日自动调用)",
		Long: `升级 mihomo 内核与 metacubexd 面板。默认两者都升;全成功才更新 .last-upgrade。

内核流程:查最新 release(经本机代理,失败直连)→ 下载(amd64 按 avx2 优选 v3,失败降级
compatible)→ 试运行校验 → 备份旧内核(保留 ` + fmt.Sprintf("%d", constants.CoreKeep) + ` 份)→ 原子替换 → 重启 →
健康检查(API 版本+出口连通)→ 失败自动回滚旧二进制。`,
		Example: "  panixy upgrade --check            # 只看可升级项\n  sudo panixy upgrade --core         # 只升内核\n  sudo panixy upgrade --core-version v1.19.31",
		RunE:    func(cmd *cobra.Command, args []string) error { return notImplemented("upgrade") },
	}
	c.Flags().Bool("core", false, "仅升级内核")
	c.Flags().Bool("ui", false, "仅升级面板(默认两者都升)")
	c.Flags().String("core-version", "", "指定内核版本(如 v1.19.31)")
	c.Flags().String("ui-version", "", "指定面板版本")
	c.Flags().Bool("check", false, "dry-run:显示当前/最新版本与将执行动作")
	return c
}

func cmdUpdateUI() *cobra.Command {
	return &cobra.Command{
		Use:   "update-ui",
		Short: "仅升级 metacubexd 面板(等价 upgrade --ui)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("update-ui") },
	}
}

func cmdRollback() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback [版本]",
		Short: "回滚内核二进制(默认最近备份;UI 靠升级事务内自动回滚)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("rollback") },
	}
}

func cmdUninstall() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "停止服务、清理防火墙与 systemd 单元(保留 /opt 数据与配置)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("uninstall") },
	}
}

func cmdUnits() *cobra.Command {
	return &cobra.Command{
		Use:   "units",
		Short: "输出渲染后的 systemd 单元文本(离线审查,不动系统)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("units") },
	}
}

func cmdLog() *cobra.Command {
	return &cobra.Command{
		Use:   "log [行数]",
		Short: "查看 panixy/mihomo 服务日志(journalctl)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("log") },
	}
}

func cmdCheck() *cobra.Command {
	return &cobra.Command{
		Use:   "check [yaml]",
		Short: "用 mihomo -t 校验配置(默认当前配置;只读免 root)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("check") },
	}
}

func cmdApplyConf() *cobra.Command {
	return &cobra.Command{
		Use:   "apply-conf <yaml>",
		Short: "应用自定义配置(优先热重载;注意热重载不刷新 proxy-providers)",
		RunE:  func(cmd *cobra.Command, args []string) error { return notImplemented("apply-conf") },
	}
}

func cmdFw() *cobra.Command {
	c := &cobra.Command{
		Use:   "fw <apply|teardown|clean>",
		Short: "防火墙管理(高级;服务单元自动调用)",
		Long: `防火墙 DNS 劫持管理(systemd 单元自动调用,一般无需手工执行):

  apply     无条件清理自有表后加载当前模式完整规则(幂等;kill -9 残留随服务 restart 自愈)
  teardown  删除自有全部表/链/策略路由(服务停止时调用)
  clean     仅清理不加载`,
		Args: cobra.ExactValidArgs(1),
		ValidArgs: []string{"apply", "teardown", "clean"},
		RunE: func(cmd *cobra.Command, args []string) error {
			fw, err := firewall.New()
			if err != nil {
				return err
			}
			// tproxy 模式下 apply 需加载完整规则(读状态文件;缺省 tun)
			mode := statemode.Read(paths.Get().State)
			switch args[0] {
			case "apply":
				if mode == "tproxy" {
					return fw.ApplyTproxy()
				}
				return fw.ApplyDnsHijack()
			case "teardown":
				return fw.Teardown()
			case "clean":
				return fw.CleanAll()
			}
			return nil
		},
	}
	return c
}

func cmdMan() *cobra.Command {
	return &cobra.Command{
		Use:   "man",
		Short: "显示手册(部署后也可直接 man panixy)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.MkdirTemp("", "panixy-man-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			hdr := &doc.GenManHeader{Title: "PANIXY", Section: "1", Manual: "Panixy 手册", Source: "panixy " + constants.Version}
			if err := doc.GenManTree(cmd.Root(), hdr, dir); err != nil {
				return fmt.Errorf("生成手册失败: %w", err)
			}
			b, err := os.ReadFile(dir + "/panixy.1")
			if err != nil {
				return err
			}
			// 优先交给系统 man 渲染;无 man/groff 时退化为可读纯文本(去 roff 控制行)
			if _, err := exec.LookPath("man"); err == nil {
				c := exec.Command("man", "-l", dir+"/panixy.1")
				c.Stdout, c.Stderr = os.Stdout, os.Stderr
				if c.Run() == nil {
					return nil
				}
			}
			fmt.Print(roffToText(b))
			return nil
		},
	}
}

// roffToText 极简 roff 降级:丢控制行、还原转义,保证无 man 环境仍可读。
func roffToText(b []byte) string {
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, ".") || strings.HasPrefix(t, "'") {
			continue
		}
		t = strings.ReplaceAll(t, `\-`, "-")
		t = strings.ReplaceAll(t, `\&`, "")
		out = append(out, t)
	}
	return strings.Join(out, "\n") + "\n"
}
