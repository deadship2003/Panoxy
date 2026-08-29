// Package constants 定义 panixy 全局常量:目录布局、防火墙标识、端口。
// 布局原则:/opt/panixy 自包含数据家目录;/etc/clash.yaml 是管理员手编的系统级配置(唯一事实源)。
package constants

const (
	Version = "0.1.0"

	// 以下为默认值,测试/沙箱可用环境变量覆盖(见 internal/paths)
	DefRootDir    = "/opt/panixy"
	DefConfPath   = "/etc/clash.yaml"
	DefUnitDir    = "/etc/systemd/system"
	DefCliDest    = "/usr/local/bin/panixy"
	DefManGz      = "/usr/local/share/man/man1/panixy.1.gz"
	DefSysctlFile = "/etc/sysctl.d/99-panixy.conf"
	DefLockFile   = "/run/panixy.lock"

	// 防火墙:独立表,绝不复用系统 nat/filter 表;启动无条件 CleanAll 实现 restart 自愈
	NftFamily = "inet"
	NftTable  = "panixy"

	MarkSelf    = 6666 // mihomo routing-mark:自身出站流量标记,防火墙据此放行防 DNS 回环(勿改,与配置模板联动)
	MarkTproxy  = 1    // TPROXY 模式流量标记
	TproxyTable = 100  // TPROXY 策略路由表号
	TproxyPort  = 7893 // mihomo tproxy-port

	DnsListenPort = 1053 // mihomo DNS 监听端口(防火墙 redirect 目标)
	MixedPortDef  = 33833
	ApiPortDef    = 9999
	DefSecret     = "deadship" // 面板/API 默认密钥(init/deploy --secret 默认值,与 API 客户端回退同源)

	CoreKeep = 3 // 内核备份保留份数
)
