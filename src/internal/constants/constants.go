// Package constants 定义 panixy 全局常量:目录布局、防火墙标识、端口。
// 布局原则:/opt/<ProgName> 自包含数据家目录;/etc/<ProgName>.yaml 是管理员手编的系统级配置(唯一事实源)。
package constants

import "strings"

// ProgName 程序名:编译期可用
//
//	-ldflags "-X github.com/deadship2003/Panoxy/internal/constants.ProgName=myproxy"
//
// 注入。一旦注入,派生路径/单元名/防火墙表链/env 前缀/状态文件/备份后缀全部跟随(见 EnvPrefix)。
// 缺省 "Panoxy"(Makefile 的 PROG / build.sh 的 --prog 亦默认此名)。
var ProgName = "Panoxy"

// EnvPrefix 环境变量前缀:PANOXY_ → <PROG>_(小写转大写,- 转 _)。
func EnvPrefix() string { return strings.ToUpper(strings.ReplaceAll(ProgName, "-", "_")) }

const (
	Version = "0.0.1"

	// 以下为默认值,测试/沙箱可用环境变量覆盖(见 internal/paths)
	DefUnitDir = "/etc/systemd/system"

	// 防火墙:独立表,绝不复用系统 nat/filter 表;启动无条件 CleanAll 实现 restart 自愈
	NftFamily = "inet"

	MarkSelf    = 6666 // mihomo routing-mark:自身出站流量标记,防火墙据此放行防 DNS 回环(勿改,与配置模板联动)
	MarkTproxy  = 1    // TPROXY 模式流量标记
	TproxyTable = 100  // TPROXY 策略路由表号
	TproxyPort  = 7893 // mihomo tproxy-port

	DnsListenPort = 1053 // mihomo DNS 监听端口(防火墙 redirect 目标)
	MixedPortDef  = 33833
	ApiPortDef    = 9999
	DefSecret     = "deadship" // 面板/API 默认密钥(init/deploy --secret 默认值,与 API 客户端回退同源)
)

// mihomo 上游内嵌内核基线:Alpha 分支锁定 commit(subtree 引入的版本,即 third_party/mihomo 内容对应)。
// upstream 命令据此探测上游是否有新提交;subtree 同步后需同步更新此常量与 third_party/mihomo/.git-subtree-source。
const (
	UpstreamRepo         = "https://github.com/MetaCubeX/mihomo"
	UpstreamBranch       = "Alpha"
	UpstreamMihomoCommit = "65287f0"
)

// 以下默认值随 ProgName 派生(编译注入 ProgName 后自动跟随)。
var (
	DefRootDir    = "/opt/" + ProgName
	DefConfPath   = "/etc/" + ProgName + ".yaml"
	DefCliDest    = "/usr/local/bin/" + ProgName
	DefManGz      = "/usr/local/share/man/man1/" + ProgName + ".1.gz"
	DefSysctlFile = "/etc/sysctl.d/99-" + ProgName + ".conf"
	DefLockFile   = "/run/" + ProgName + ".lock"
	NftTable      = ProgName
)

// BackupSuffix / PremergeSuffix 备份后缀:随程序名派生(config 事务备份与 merge 预合并备份共用)。
func BackupSuffix() string   { return "." + ProgName + "-bak" }
func PremergeSuffix() string { return "." + ProgName + "-premerge" }
