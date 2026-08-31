// Package core 进程内封装 mihomo 内核(Alpha 分支),与外部 mihomo 二进制等价。
//
// M2 起生命周期切换为进程内:systemd 单元 ExecStart 直接跑 Run,不再启动外部二进制。
// Run 严格对应上游 main.go 的启动段 + 信号主循环;Validate 对应 -t 校验。
// 禁止私自增删逻辑(细节见 [[mihomo-alpha-embedding]])。
package core

import (
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"go.uber.org/automaxprocs/maxprocs"

	"github.com/metacubex/mihomo/component/updater"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub"
	"github.com/metacubex/mihomo/hub/executor"
	"github.com/metacubex/mihomo/log"
)

// Run 进程内启动内核并阻塞,等价 mihomo main() 的启动段 + 信号主循环:
// PreferGo → maxprocs → SetHomeDir → SetConfig → config.Init → hub.Parse(nil) →
// geo 自动更新 → 信号循环(SIGHUP 重读文件 reload / SIGINT·SIGTERM → executor.Shutdown)。
// configPath 为空时退回 homeDir/config.yaml(等价 -f 缺省)。opts 透传 external-ui 等覆盖项。
func Run(homeDir, configPath string, opts ...hub.Option) error {
	net.DefaultResolver.PreferGo = true
	_, _ = maxprocs.Set(maxprocs.Logger(func(string, ...any) {}))

	if homeDir != "" {
		C.SetHomeDir(homeDir)
	}
	if configPath == "" {
		configPath = filepath.Join(C.Path.HomeDir(), C.Path.Config())
	}
	C.SetConfig(configPath)
	if err := config.Init(C.Path.HomeDir()); err != nil {
		return err
	}
	if err := hub.Parse(nil, opts...); err != nil {
		return err
	}
	if updater.GeoAutoUpdate() {
		updater.RegisterGeoUpdater()
	}

	defer executor.Shutdown()
	termSign := make(chan os.Signal, 1)
	hupSign := make(chan os.Signal, 1)
	signal.Notify(termSign, syscall.SIGINT, syscall.SIGTERM)
	signal.Notify(hupSign, syscall.SIGHUP)
	for {
		select {
		case <-termSign:
			return nil
		case <-hupSign:
			if err := hub.Parse(nil, opts...); err != nil {
				log.Errorln("Parse config error: %s", err.Error())
			}
		}
	}
}

// Validate 等价 mihomo -t:仅解析校验配置,不启动任何监听。
// homeDir 用于解析 geodata(GeoSite.dat/GeoIP.dat)等相对资源;为空则用默认 home。
func Validate(homeDir string, configBytes []byte) error {
	if homeDir != "" {
		C.SetHomeDir(homeDir)
	}
	_, err := executor.ParseWithBytes(configBytes)
	return err
}
