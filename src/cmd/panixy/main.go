// panixy — 基于 mihomo 的 Linux 透明代理网关部署/管理工具(Go 版单二进制)。
// 职责边界:模板渲染、文件/单元管理、防火墙 DNS 劫持、订阅管理、升级回滚、健康检测;
// 业务转发与 DNS 解析全部由 mihomo 内核完成。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/spf13/pflag"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/firewall"
	"github.com/deadship2003/Panoxy/internal/logx"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/statemode"
)

// version 由构建脚本经 -ldflags -X 注入;缺省用常量。
var version = constants.Version

func main() {
	// cobra 不自带 -?:入口处归一化
	for i, a := range os.Args {
		if a == "-?" {
			os.Args[i] = "--help"
		}
	}
	if err := NewRootCmd().Execute(); err != nil {
		logx.Error("%v", err)
		cleanExit(1)
	}
	cleanExit(0)
}

// cleanExit 确保所有命令执行后终端干净返回 prompt:
// 进度条的 \r 残留、后台进程的 stdin 持有,都会导致 shell 不显示提示符。
func cleanExit(code int) {
	os.Stdout.Sync()
	os.Stderr.Sync()
	os.Exit(code)
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "panixy",
		Short: "基于 mihomo 的透明代理网关部署/管理工具",
		Long: `panixy — 基于 mihomo(TUN/TPROXY)的 Linux 透明代理网关部署/管理工具

数据面(节点/策略组选择)在 Web 面板;传输面(tun/tproxy 模式、防火墙)在本 CLI。

引导:
  panixy init --dry-run                  # 试运行模式(不需要 root)
  panixy try '订阅链接'                   # 沙箱实测完整安装(不需要 root)
  sudo panixy init '订阅链接'             # 直接初始化部署
  sudo ./panixy deploy '订阅链接'         # 从离线包部署

订阅/配置:
  sudo panixy sub import '订阅链接'        # 导入订阅(回车粘贴,免引号)
  sudo panixy merge-conf ~/my.yaml        # 融合个人配置(--dry-run 预览)
  panixy config                            # 打印默认配置模板(免 root)

日常:
  panixy status                          # 健康一览(服务/防火墙/订阅/出网)
  sudo panixy mode tproxy                # 切换 TPROXY 模式(需内核 xt_TPROXY)
  sudo panixy upgrade --check            # 查看可升级项

运维:
  sudo panixy redeploy                   # 就地强制刷新全部程序文件(保留配置)
  sudo panixy rollback                   # 内核回滚到最近备份
  sudo panixy uninstall                  # 卸载(保留数据与配置)

详细说明: panixy man 或 man panixy-<命令>(部署后可用)`,
		Version: version,
	}
	root.PersistentFlags().String("root", "", "安装目录(默认 /opt/panixy;数据家目录可整体重定位,/etc/clash.yaml 仍为系统级配置)")
	root.PersistentFlags().Bool("verbose", false, "分步明细:每个事务步骤、写入的文件、应用的规则")
	root.PersistentFlags().Bool("debug", false, "全量透传:外部命令原样回显、mihomo API 请求响应、配置 diff")
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if r, _ := cmd.Flags().GetString("root"); r != "" {
			if !filepath.IsAbs(r) {
				logx.Error("--root 需要绝对路径: %s", r)
				os.Exit(1)
			}
			os.Setenv(constants.EnvPrefix()+"_ROOT", r) // 所有 paths.Get() 即时生效;服务单元亦会注入
		}
		if d, _ := cmd.Flags().GetBool("debug"); d {
			logx.SetLevel(logx.LevelDebug)
		} else if v, _ := cmd.Flags().GetBool("verbose"); v {
			logx.SetLevel(logx.LevelVerbose)
		}
	}
	root.AddCommand(
		cmdInit(), cmdDeploy(), cmdRedeploy(), cmdSub(),
		cmdTry(), cmdMergeConf(), cmdStatus(), cmdMode(), cmdUpgrade(), cmdRollback(),
		cmdUninstall(), cmdUnits(), cmdLog(), cmdCheck(), cmdApplyConf(), cmdConfig(),
		cmdFw(), cmdMan(),
	)
	rebrand(root) // 把硬编码的 "panixy"/"/etc/clash.yaml" 替换为编译期注入的 ProgName/DefConfPath
	return root
}

// rebrand 把命令树中硬编码的 "panixy" 与 "/etc/clash.yaml" 替换为编译期注入的
// ProgName / DefConfPath,使 --help/man 示例、flag 说明与改名后的程序一致。
func rebrand(cmd *cobra.Command) {
	rep := func(s string) string {
		s = strings.ReplaceAll(s, "panixy", constants.ProgName)
		s = strings.ReplaceAll(s, "/etc/clash.yaml", constants.DefConfPath)
		return s
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.Use = rep(c.Use)
		c.Short = rep(c.Short)
		c.Long = rep(c.Long)
		c.Example = rep(c.Example)
		c.Flags().VisitAll(func(f *pflag.Flag) { f.Usage = rep(f.Usage) })
		c.PersistentFlags().VisitAll(func(f *pflag.Flag) { f.Usage = rep(f.Usage) })
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(cmd)
}

// upperFirst 首字母大写(程序名为 ASCII 二进制/文件名,安全)。
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// manHeader 生成 man 页头:标题/手册名/来源均随程序名派生。
func manHeader() *doc.GenManHeader {
	return &doc.GenManHeader{
		Title:   strings.ToUpper(constants.ProgName),
		Section: "1",
		Manual:  upperFirst(constants.ProgName) + " 手册",
		Source:  constants.ProgName + " " + version,
	}
}

func cmdTry() *cobra.Command {
	c := &cobra.Command{
		Use:   "try [订阅URL]",
		Short: "预安装:免 root 沙箱实测完整安装流程(通过=可放心真装)",
		Long: `预安装(测试安装,不需要 root):把 init 的全流程在沙箱里真跑一遍 ——
真实下载资产、真实启动 mihomo 内核、真实导入订阅并验证节点数>0、真实健康检查;
一切落在沙箱目录,不触碰真实系统(不写 /opt、/etc,不装服务,不动防火墙)。

沙箱与真实部署仅两处差异(均为非 root 限制,真机 sudo 部署不存在):
  - 内核引导时剥离 tun 段(TUN 建设备需 CAP_NET_ADMIN)
  - 剥离 routing-mark(SO_MARK 需权限);防火墙规则不落地
通过后,真实部署执行: sudo panixy init '订阅URL'`,
		Example: `  panixy try 'https://example.com/sub?token=x'   # 全流程实测
  panixy try --dir ~/panixy-sandbox             # 指定沙箱目录(默认临时目录)
  panixy try                                    # 回车粘贴订阅`,
		RunE: runTry,
	}
	addSubSourceFlags(c)
	addDeployFlags(c)
	addDownloadFlags(c)
	c.Flags().String("dir", "", "沙箱目录(默认 /tmp/panixy-try-<时间戳>)")
	return c
}

func cmdMergeConf() *cobra.Command {
	c := &cobra.Command{
		Use:   "merge-conf <个人配置.yaml>",
		Short: "个人配置叠加融合:同名组合并、新增追加、基底保留;备份可回滚",
		Long: `把个人 clash.yaml(文件名任意)叠加融合到默认模板(config.default.yaml)上 —— 同名组合并而非替换。

基底:    /opt/panixy/config.default.yaml(init/deploy 生成的干净模板,含 SUB_URL_PLACEHOLDER)
组融合:  同名 → 字段级合并(proxies/use 取并集,标量个人覆盖)
          个人新增组 → 追加到末尾
          基底组(地区组/应用组)→ 保留不删(引用不断链)
规则融合: 个人规则前置(优先匹配)+ 基底规则兜底(MATCH 排最后,去重)
订阅合并: 个人订阅追加;占位订阅(SUB_URL_PLACEHOLDER)自动退场
接管(个人): 端口/密钥/external-controller/proxies
保留(基底): tun 模式段/routing-mark/dns.listen(暗号)/geo/ntp/sniffer
自动:     PROCESS- 规则 → find-process-mode=strict;占位订阅退场

备份与回滚:
  融合前自动备份 → /etc/clash.yaml.panixy-premerge
  任一步失败自动恢复;成功后可用 --rollback 手动回滚到融合前`,
		Example: `  panixy merge-conf --dry-run ~/my-clash.yaml    # 试运行(不落盘不备份)
  sudo panixy merge-conf ~/my-clash.yaml         # 融合并生效
  sudo panixy merge-conf --rollback              # 回滚到融合前
  sudo panixy merge-conf --dns mine ~/my-clash.yaml`,
		RunE: runMergeConf,
	}
	addDryRunFlag(c, "试运行模式:只输出决策报告与融合结果预览,不落盘不备份")
	c.Flags().String("dns", "keep", "DNS 段策略: keep(基底)| mine(个人,listen 强制 1053)")
	c.Flags().Bool("no-wire", false, "不把基底订阅自动接线进组")
	c.Flags().Bool("rollback", false, "从 .panixy-premerge 备份恢复到融合前")
	return c
}

func cmdInit() *cobra.Command {
	c := &cobra.Command{
		Use:   "init [订阅URL]",
		Short: "不打包直接初始化:单二进制裸机下载资产+部署+导订阅(自带进度;--dry-run 试运行)",
		Long: `不打包、不用离线资产的单二进制初始化 —— 在任何裸机上直接完成部署。

下载三级策略(每一步带进度条,--verbose 看分步,--debug 看全部细节):
  直连(15s 失败硬顶)> 订阅引导代理(用订阅节点起本地代理;需本机已有
  任意 mihomo 内核,--boot-bin 指定,默认 /opt/panixy/bin/mihomo)> gh 镜像
  (--mirror,第三方源,内核会做试运行校验;给朋友用建议离线包 deploy)

九步:预检 → 取订阅 → 网络探测 → 下载内核(按架构/AVX2 降级)→ geo/规则
→ 面板 → 资产就位+渲染配置 → 部署服务(防火墙/健康)→ 导入订阅(节点数>0)。`,
		Example: `  sudo panixy init 'https://example.com/sub?token=x&sid=y'
  sudo panixy init --name Nano                            # 回车粘贴订阅
  sudo panixy init --file sub.yaml URL                    # 订阅离线导入
  sudo panixy init --mirror https://ghfast.top/ URL       # 直连不通时
  panixy init --dry-run                                   # 试运行模式(不需要 root)`,
		RunE: runInit,
	}
	addSubSourceFlags(c)
	addDeployFlags(c)
	addDownloadFlags(c)
	addDryRunFlag(c, "试运行模式:环境/下载策略/落位/配置渲染预览,不执行不需要 root")
	return c
}

func cmdDeploy() *cobra.Command {
	c := &cobra.Command{
		Use:   "deploy [订阅URL]",
		Short: "全新部署(离线包内运行;--dry-run 试运行)",
		Long: `全新部署,须在解压的离线包根目录运行。

流程:放置内核/geo/面板/广告规则 → 渲染配置(现有 > 包内手工 > 模板)→
安装 CLI 与 man 手册 → 写入 systemd 单元 → 开启 ip_forward → 拉起服务(含防火墙)。
任一步失败全量回滚。检测到 bash 旧版部署残留(unit 含 resolvectl/配置含 dns-hijack)时中止并给出手动清理指引。`,
		Example: `  sudo ./panixy deploy 'https://example.com/sub?token=x&sid=y'   # 部署并导入订阅
  sudo ./panixy deploy --name Nano                              # 部署,回车粘贴订阅
  sudo ./panixy deploy --proxy-mode tproxy                      # 以 TPROXY 模式部署`,
		RunE: runDeploy,
	}
	addSubSourceFlags(c)
	addDeployFlags(c)
	addDryRunFlag(c, "试运行模式:环境/资产/下载策略/配置渲染预览,不执行不需要 root")
	return c
}

func cmdSub() *cobra.Command {
	c := &cobra.Command{
		Use:   "sub",
		Short: "订阅管理:导入/删除/列出 proxy-providers",
		Long: `管理 mihomo 订阅(proxy-providers):导入或更换、删除、查看状态与节点数。

订阅导入经 yaml 增量编辑写入 proxy-providers[NAME](复用锚点 <<: *p),并预置缓存、
重启内核、验证节点数>0,任一步失败自动回滚。`,
		Example: `  sudo panixy sub import 'https://example.com/sub?token=x'   # 导入(粘贴模式免引号)
  sudo panixy sub import --name airport2 'https://example.com/sub2'
  sudo panixy sub del --name airport2
  panixy sub list`,
	}
	c.AddCommand(cmdSubImport(), cmdSubDel(), cmdSubList())
	return c
}

func cmdSubImport() *cobra.Command {
	c := &cobra.Command{
		Use:   "import [订阅URL]",
		Short: "导入/更换订阅:预取→预置缓存→重启→验证节点数>0",
		Long: `导入或更换订阅。无参数时进入粘贴模式(读整行,URL 含 & ? 等字符无需引号)。

流程:预拉取(本地文件 > 直连 > 经本机代理)→ 校验为含节点的 Clash YAML →
以 yaml 增量编辑写入 proxy-providers[NAME](复用锚点 <<: *p,保留其他 provider 与注释,
并把 NAME 融合进各组 use 列表)→ mihomo -t 校验 → 预置 provider 缓存 → 重启
(热重载不刷新 provider,mihomo 限制)→ 查询该 provider 节点数,=0 自动回滚。

前置要求:配置中存在锚点 &p(基础模板自带)。`,
		Example: "  sudo panixy sub import --name airport2 'https://example.com/sub2'\n  sudo panixy sub import   # 粘贴模式",
		RunE:    runSubImport,
	}
	addSubSourceFlags(c)
	c.Flags().StringSlice("group", nil, "限定融合的组(默认:全部 use 非空的组/锚点持有者)")
	return c
}

func cmdSubDel() *cobra.Command {
	c := &cobra.Command{
		Use:   "del --name NAME",
		Short: "删除指定订阅 provider(备份、校验、重启;失败回滚)",
		Long: `从 proxy-providers 中删除指定订阅,并把它从各组 use 列表移除。

事务流程:备份配置 → 删除 provider + 取消接线 → mihomo -t 校验 → 重启 → 健康检查,
任一步失败自动回滚。注意:删光唯一订阅会使组失去 use,-t 会拒绝(此时先导入新订阅)。

provider 名称即 sub import 时的 --name(默认 SUB);现有名称用 sub list 查看。`,
		Example: "  sudo panixy sub del --name airport2",
		RunE:    runSubDel,
	}
	c.Flags().String("name", "", "要删除的 provider 名称(必填)")
	_ = c.MarkFlagRequired("name")
	return c
}

func cmdSubList() *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "列出全部订阅:状态/节点数/错误(单订阅故障不影响其他展示)",
		Long: `读取配置全部 proxy-providers,逐个调用 mihomo API 查询状态。

状态:✅正常 / ⚠️获取失败 / ⚠️解析失败 / ⚠️节点为0。--json 输出机器可读格式。`,
		Example: "  panixy sub list            # 表格\n  panixy sub list --json     # 机器可读",
		RunE:    runSubList,
	}
	c.Flags().Bool("json", false, "以 JSON 输出")
	return c
}

func cmdStatus() *cobra.Command {
	c := &cobra.Command{
		Use:   "status [-q|--json|--detail]",
		Short: "健康一览:服务/防火墙/各订阅节点数/内核UI版本/出口连通",
		Long: `健康一览。包含:服务状态、防火墙后端与残留规则、全部 proxy-providers 状态、
内核/UI 版本、上次升级时间、代理出口连通性;并提示浏览器 DoH 无法被内核劫持。

  --detail  追加明细:当前代理模式(tun/tproxy)、TUN 栈风险提示、路由/缓存细节
  -q  静默,仅退出码:0健康 1降级(节点0或代理出网不通) 2故障(服务/API 不可用)
  --json 机器可读单行`,
		Example: `  panixy status              # 一览
  panixy status --detail      # 追加明细
  panixy status -q            # 仅退出码(监控脚本)
  panixy status --json        # 机器可读`,
		RunE: runStatus,
	}
	c.Flags().Bool("detail", false, "追加明细")
	c.Flags().BoolP("quiet", "q", false, "静默,仅退出码")
	c.Flags().Bool("json", false, "以 JSON 输出")
	return c
}

func cmdMode() *cobra.Command {
	return &cobra.Command{
		Use:   "mode [tun|tproxy]",
		Short: "查看或切换透明代理模式(原子切换:防火墙+配置+重启)",
		Long: `查看或切换 tun/tproxy 模式。

切换是原子事务:卸载旧防火墙规则 → 渲染对应配置变体 → -t 校验 → 重启服务 →
加载新防火墙 → 健康检查,任一步失败整体回滚。TPROXY 前置依赖内核 xt_TPROXY
模块,不可用时拒绝切换。

流量策略(两种模式统一):
  不阻断任何协议(QUIC/DoT/DoQ/DoH 均纳入正常分流)
  DNS 53 劫持(为大多数设备提供域名级分流)
  32 条基础服务直连:SSH(22) RDP(3389) VNC(5900)
    VPN(Tailscale/WG/OpenVPN/IPSec/L2TP/PPTP) VoIP(SIP) 域认证(Kerberos/LDAP)
    IoT(MQTT/CoAP) 存储(iSCSI/MySQL/PG/Redis/Mongo) 等

TUN(默认) vs TPROXY 选型:
  TUN:    简单稳定,auto-route 自动处理路由;家用推荐
          源 IP 丢失(全部显示为网关 IP)
          WSL2/虚拟化/Docker 兼容性好
  TPROXY: 保留客户端真实 IP(日志可看到每台设备)
          内核直接转发,弱 CPU 设备性能更优
          需内核 xt_TPROXY 模块;Docker 容器可能被误劫持
          IPv6 策略路由需额外注意

TPROXY 前置检测:
  grep -w TPROXY /proc/net/ip_tables_targets
  sudo modprobe xt_TPROXY && grep -w TPROXY /proc/net/ip_tables_targets

切换后验证:
  ip rule show | grep fwmark          # 应有 fwmark 0x1 lookup 100
  ip route show table 100             # 应有 local default dev lo
  sudo nft list table inet panixy | grep tproxy

透明网关网络配置(LAN 设备接入):
  路由器 DHCP 下发网关 = panixy 机器 LAN 口 IP,DNS 下发公网地址(53 会被劫持);
  或单台设备手动设置网关指向 panixy 机器。

注意:模式无法在 Web 面板切换 —— 防火墙规则与配置必须同事务变更,面板只管数据面(节点/组)。
不带参数则显示当前模式。`,
		Example: `  panixy mode              # 查看当前模式
  sudo panixy mode tproxy  # 切换到 TPROXY(需内核 xt_TPROXY)
  sudo panixy mode tun     # 切回 TUN(默认)`,
		RunE: func(cmd *cobra.Command, args []string) error { return runMode(cmd, args) },
	}
}

func cmdUpgrade() *cobra.Command {
	c := &cobra.Command{
		Use:   "upgrade [--core|--ui|--cli] [--core-version vX] [--ui-version vX] [--check]",
		Short: "升级内核/面板/CLI 自身(timer 每日自动调用)",
		Long: `升级 mihomo 内核、metacubexd 面板(默认两者都升;--cli 显式升 CLI 自身)。全成功才更新 .last-upgrade。
CLI 升级(--cli):在源码树内本地自编译(go build)→ 备份旧版 → 替换已装二进制;不下载预编译产物(--src 指仓库根,缺省当前目录)。

内核流程:查最新 release(经本机代理,失败直连)→ 下载(amd64 按 avx2 优选 v3,失败降级
compatible)→ 试运行校验 → 备份旧内核(保留 ` + fmt.Sprintf("%d", constants.CoreKeep) + ` 份)→ 原子替换 → 重启 →
健康检查(API 版本+出口连通)→ 失败自动回滚旧二进制。`,
		Example: "  panixy upgrade --check            # 只看可升级项\n  sudo panixy upgrade --core         # 只升内核\n  sudo panixy upgrade --cli          # 只升 CLI 自身\n  sudo panixy upgrade --core-version v1.19.31",
		RunE:    runUpgrade,
	}
	c.Flags().Bool("core", false, "仅升级内核")
	c.Flags().Bool("ui", false, "仅升级面板(默认两者都升)")
	c.Flags().String("core-version", "", "指定内核版本(如 v1.19.31)")
	c.Flags().String("ui-version", "", "指定面板版本")
	c.Flags().Bool("check", false, "dry-run:显示当前/最新版本与将执行动作")
	c.Flags().Bool("cli", false, "仅升级 CLI(panixy 自身;本地自编译)")
	c.Flags().String("src", "", "源码根目录(--cli 自编译用;默认当前目录)")
	return c
}

func cmdRollback() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback [版本]",
		Short: "回滚内核二进制到某备份版本(默认最近一份;失败可反复回滚)",
		Long: `把 mihomo 内核二进制回滚到升级时留下的备份(保留最近 ` + fmt.Sprintf("%d", constants.CoreKeep) + ` 份)。

不带参数回滚到最近备份;带版本号(如 v1.19.30)回滚到指定备份。回滚前把当前内核另存为
备份,因此可反复回滚;回滚后自动重启并做健康检查(失败仅告警,不阻断)。

面板(UI)回滚由升级事务内自动处理,此处只管内核。`,
		Example: `  sudo panixy rollback              # 回滚到最近备份
  sudo panixy rollback v1.19.30    # 回滚到指定版本`,
		RunE: runRollback,
	}
}

func cmdUninstall() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "停止服务、清理防火墙与 systemd 单元(保留 /opt 数据与配置)",
		Long: `停止并移除 panixy 服务与定时升级任务,清理自有防火墙规则、sysctl 与手册。

保留:/opt/panixy 数据目录(内核/geo/面板/订阅缓存)与 /etc/clash.yaml 配置,
以及 CLI 二进制本身 —— 卸载后重新 init/deploy 即可复用数据。`,
		Example: "  sudo panixy uninstall",
		RunE:    runUninstall,
	}
}

func cmdUnits() *cobra.Command {
	return &cobra.Command{
		Use:   "units",
		Short: "输出渲染后的 systemd 单元文本(离线审查,不动系统)",
		Long: `打印 panixy.service / panixy-upgrade.service / panixy-upgrade.timer 的完整单元文本,
按当前安装目录(--root)渲染。只读,不写任何文件,便于安装前审查或 diff。`,
		Example: "  panixy units > units.txt    # 导出审查",
		RunE:    runUnits,
	}
}

func cmdLog() *cobra.Command {
	return &cobra.Command{
		Use:   "log [行数]",
		Short: "查看 panixy/mihomo 服务日志(journalctl)",
		Long: `透传 journalctl 查看 panixy.service 与 panixy-upgrade.service 的最近日志。
不带参数显示最近 80 行;带数字参数指定行数。`,
		Example: "  panixy log        # 最近 80 行\n  panixy log 200    # 最近 200 行",
		RunE:    runLog,
	}
}

func cmdCheck() *cobra.Command {
	return &cobra.Command{
		Use:   "check [yaml]",
		Short: "用 mihomo -t 校验配置语法(默认当前配置;只读免 root)",
		Long: `用 mihomo -t 校验配置,透传内核报错的首条 error。只读,不改任何文件、免 root。

不带参数校验当前 /etc/clash.yaml;带路径校验指定文件(如 apply-conf 前先验)。`,
		Example: "  panixy check                 # 校验当前配置\n  panixy check ~/my-clash.yaml  # 校验指定文件",
		RunE:    runCheck,
	}
}

func cmdApplyConf() *cobra.Command {
	return &cobra.Command{
		Use:   "apply-conf <yaml>",
		Short: "应用自定义配置(优先热重载;注意热重载不刷新 proxy-providers)",
		Long: `校验通过后把指定 YAML 应用到 /etc/clash.yaml:优先热重载(仅非 provider 改动有效),
热重载未生效则退重启,再失败恢复原配置。应用前自动备份,成功清除备份。

注意:mihomo 热重载不刷新 proxy-providers,改动订阅相关字段需重启才生效。`,
		Example: "  sudo panixy apply-conf ~/my-clash.yaml",
		RunE:    runApplyConf,
	}
}

func cmdConfig() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "渲染/打印默认配置模板(免 root;--write 写回 config.default.yaml)",
		Long: `渲染内嵌默认模板(config.tpl)并打印到 stdout —— 与 init/deploy 首次落盘的 /etc/clash.yaml
同源,保留 SUB_URL_PLACEHOLDER、不含任何订阅。

默认密钥/端口:secret=deadship、mixed-port=33833、HTTP 9966、SOCKS 6699、API 9999。
--mode tun|tproxy 决定 tun/tproxy 变体;--secret 覆盖面板密钥。

--write 额外把渲染结果写回 /opt/panixy/config.default.yaml(纯净默认副本,供
merge-conf 重建基线),需对安装目录有写权限(通常 sudo)。

只读、不部署、不动防火墙/服务;无需 root(除非 --write)。`,
		Example: `  panixy config                       # 打印默认配置(stdout)
  panixy config > clash.yaml          # 导出到文件
  panixy config --mode tproxy         # TPROXY 变体
  sudo panixy config --write          # 写回 config.default.yaml`,
		RunE: runConfig,
	}
	c.Flags().String("mode", "tun", "透明代理模式: tun | tproxy")
	c.Flags().String("secret", constants.DefSecret, "面板/API 密钥")
	c.Flags().Bool("write", false, "写回 /opt/panixy/config.default.yaml(默认仅打印)")
	return c
}

func cmdFw() *cobra.Command {
	c := &cobra.Command{
		Use:   "fw <apply|teardown|clean>",
		Short: "防火墙管理(高级;服务单元自动调用)",
		Long: `防火墙 DNS 劫持管理(systemd 单元自动调用,一般无需手工执行):

  apply     无条件清理自有表后加载当前模式完整规则(幂等;kill -9 残留随服务 restart 自愈)
  teardown  删除自有全部表/链/策略路由(服务停止时调用)
  clean     仅清理不加载`,
		Args:      cobra.ExactValidArgs(1),
		ValidArgs: []string{"apply", "teardown", "clean"},
		Example:   "  sudo panixy fw apply     # 幂等重挂当前模式规则\n  sudo panixy fw teardown  # 拆除规则\n  sudo panixy fw clean     # 仅清理不加载",
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
	c := &cobra.Command{
		Use:   "man [命令] [--raw]",
		Short: "显示手册(根页或子命令页)",
		Long: `在终端显示手册页。不带参数显示根页;带命令名显示对应子命令页(如 man init、
man sub)。优先交给系统 man 渲染,无 man 环境时降级为纯文本。

部署后同样可用系统 man:man panixy / man panixy-<命令>。--raw 输出原始 roff 供部署安装手册用。`,
		Example: "  panixy man          # 根页\n  panixy man init     # init 命令页\n  panixy man sub       # sub 命令页",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.MkdirTemp("", constants.ProgName+"-man-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			hdr := manHeader()
			if err := genAllMan(cmd.Root(), hdr, dir); err != nil {
				return fmt.Errorf("生成手册失败: %w", err)
			}
			page := constants.ProgName + ".1"
			if len(args) > 0 { // 子命令页:<prog> man init → <prog>-init.1
				page = constants.ProgName + "-" + args[0] + ".1"
			}
			b, err := os.ReadFile(dir + "/" + page)
			if err != nil {
				pages, _ := filepath.Glob(dir + "/" + constants.ProgName + "*.1")
				names := []string{}
				for _, f := range pages {
					n := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), constants.ProgName), ".1")
					if n == "" {
						names = append(names, "(root)")
					} else {
						names = append(names, strings.TrimPrefix(n, "-"))
					}
				}
				return fmt.Errorf("无 %q 的手册页;可用: %v", args[0], names)
			}
			// --raw:输出原始 roff(installMan 用来生成系统手册)
			if raw, _ := cmd.Flags().GetBool("raw"); raw {
				os.Stdout.Write(b)
				return nil
			}
			// 优先交给系统 man 渲染;无 man/groff 时退化为可读纯文本(去 roff 控制行)
			if _, err := exec.LookPath("man"); err == nil {
				c := exec.Command("man", "-l", dir+"/"+page)
				c.Stdout, c.Stderr = os.Stdout, os.Stderr
				if c.Run() == nil {
					return nil
				}
			}
			fmt.Print(roffToText(b))
			return nil
		},
	}
	c.Flags().Bool("raw", false, "输出原始 roff(供部署安装手册用)")
	return c
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

// genAllMan 生成根页 + 全部子命令页(cobra doc.GenManTree 只渲染传入命令自身,
// 不含子命令页 —— 需递归遍历;sub 等父命令下还挂 import/del/list)。
func genAllMan(root *cobra.Command, hdr *doc.GenManHeader, dir string) error {
	var walk func(c *cobra.Command) error
	walk = func(c *cobra.Command) error {
		if err := doc.GenManTree(c, hdr, dir); err != nil {
			return err
		}
		for _, sub := range c.Commands() {
			if sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if err := walk(sub); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}
