// Package paths 解析运行时路径:默认值 + 环境变量覆盖(<PROG>_ROOT 等),供沙箱测试复用 bash 版经验。
package paths

import (
	"os"
	"path/filepath"

	"github.com/deadship2003/panoxy/internal/constants"
)

type Paths struct {
	Root        string // /opt/<prog>
	UiDir       string
	UiStamp     string
	State       string // /opt/<prog>/<prog>.yaml:自身状态(proxy-mode 等)
	Conf        string // /etc/<prog>.yaml:mihomo 配置(唯一事实源)
	DefaultConf string // /opt/<prog>/config.default.yaml:纯净默认模板副本(merge-conf 重建基线)
	UnitDir     string
	Cli         string
	ManGz       string
	Sysctl      string
	Lock        string
	LastUp      string
	Proxies     string // 订阅缓存目录
	RuleProv    string // 规则缓存目录
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Get 返回当前环境的路径集合(每次调用重新解析,环境变量即时生效)。
func Get() Paths {
	pfx := constants.EnvPrefix()
	root := env(pfx+"_ROOT", constants.DefRootDir)
	return Paths{
		Root:        root,
		UiDir:       filepath.Join(root, "ui", "official"),
		UiStamp:     filepath.Join(root, "ui", ".official.version"),
		State:       env(pfx+"_STATE", filepath.Join(root, constants.ProgName+".yaml")),
		Conf:        env(pfx+"_CONF", constants.DefConfPath),
		DefaultConf: filepath.Join(root, "config.default.yaml"),
		UnitDir:     env(pfx+"_UNIT_DIR", constants.DefUnitDir),
		Cli:         env(pfx+"_CLI", constants.DefCliDest),
		ManGz:       env(pfx+"_MAN", constants.DefManGz),
		Sysctl:      env(pfx+"_SYSCTL", constants.DefSysctlFile),
		Lock:        env(pfx+"_LOCK", constants.DefLockFile),
		LastUp:      filepath.Join(root, ".last-upgrade"),
		Proxies:     filepath.Join(root, "proxies"),
		RuleProv:    filepath.Join(root, "rule_provider"),
	}
}
