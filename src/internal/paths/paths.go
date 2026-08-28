// Package paths 解析运行时路径:默认值 + 环境变量覆盖(PANIXY_ROOT 等),供沙箱测试复用 bash 版经验。
package paths

import (
	"os"
	"path/filepath"

	"github.com/deadship2003/panixy/internal/constants"
)

type Paths struct {
	Root     string // /opt/panixy
	Bin      string // 内核
	UiDir    string
	UiStamp  string
	State    string // /opt/panixy/panixy.yaml:panixy 自身状态(proxy-mode 等)
	Conf     string // /etc/clash.yaml:mihomo 配置(唯一事实源)
	UnitDir  string
	Cli      string
	ManGz    string
	Sysctl   string
	Lock     string
	LastUp   string
	Proxies  string // 订阅缓存目录
	RuleProv string // 规则缓存目录
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Get 返回当前环境的路径集合(每次调用重新解析,环境变量即时生效)。
func Get() Paths {
	root := env("PANIXY_ROOT", constants.DefRootDir)
	return Paths{
		Root:     root,
		Bin:      filepath.Join(root, "bin", "mihomo"),
		UiDir:    filepath.Join(root, "ui", "official"),
		UiStamp:  filepath.Join(root, "ui", ".official.version"),
		State:    env("PANIXY_STATE", filepath.Join(root, "panixy.yaml")),
		Conf:     env("PANIXY_CONF", constants.DefConfPath),
		UnitDir:  env("PANIXY_UNIT_DIR", constants.DefUnitDir),
		Cli:      env("PANIXY_CLI", constants.DefCliDest),
		ManGz:    env("PANIXY_MAN", constants.DefManGz),
		Sysctl:   env("PANIXY_SYSCTL", constants.DefSysctlFile),
		Lock:     env("PANIXY_LOCK", constants.DefLockFile),
		LastUp:   filepath.Join(root, ".last-upgrade"),
		Proxies:  filepath.Join(root, "proxies"),
		RuleProv: filepath.Join(root, "rule_provider"),
	}
}
