// Package core 进程内封装 mihomo 内核(Alpha 分支),与外部 mihomo 二进制等价。
//
// 双轨并行(M1):生产 systemd 单元仍 ExecStart=mihomo 二进制(见 asset/service.tpl),
// 本包提供进程内入口,供 M2 生命周期切换与 M1 的 -t 等价校验。所有函数严格对应
// 上游 main.go 主循环,禁止私自增删逻辑(细节见 [[mihomo-alpha-embedding]])。
package core

import (
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub"
	"github.com/metacubex/mihomo/hub/executor"
)

// Start 启动内核,等价 mihomo 主循环的启动段:SetHomeDir → config.Init → hub.Parse。
// configBytes 为空时走默认配置路径(等价 -f 缺省)。opts 透传 external-controller/secret 等覆盖项。
func Start(homeDir string, configBytes []byte, opts ...hub.Option) error {
	if homeDir != "" {
		C.SetHomeDir(homeDir)
	}
	if err := config.Init(C.Path.HomeDir()); err != nil {
		return err
	}
	return hub.Parse(configBytes, opts...)
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

// Reload 等价 SIGHUP:重新 hub.Parse 热重载(与 Start 不同,不重做 SetHomeDir/config.Init)。
func Reload(configBytes []byte, opts ...hub.Option) error {
	return hub.Parse(configBytes, opts...)
}

// Shutdown 等价 SIGTERM:清理监听、回写 fake-ip 状态。
func Shutdown() {
	executor.Shutdown()
}
