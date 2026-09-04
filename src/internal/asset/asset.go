// Package asset 内嵌资源:systemd 单元模板与 mihomo 完整配置模板(tun/tproxy 双模式变体)。
package asset

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/deadship2003/panoxy/internal/constants"
)

//go:embed service.tpl upgrade-service.tpl upgrade-timer.tpl config.tpl
var files embed.FS

// UnitData 渲染主服务单元所需字段。
type UnitData struct {
	Mode                   string // tun / tproxy(仅用于 Description)
	Prog, EnvPrefix        string // 程序名 / env 前缀(随编译期 ProgName 注入)
	Conf, Root, UiDir, Cli string
}

func render(name string, data any) (string, error) {
	raw, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("embedded asset missing: %s: %w", name, err)
	}
	t, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("failed to render template %s: %w", name, err)
	}
	return b.String(), nil
}

// RenderService 渲染 <Prog>.service(即 panoxy.service;无任何 resolvectl 逻辑;
// fw apply 内部先无条件 CleanAll,kill -9 残留随 restart 自愈)。
func RenderService(d UnitData) (string, error) { return render("service.tpl", d) }

// RenderUpgradeService / RenderUpgradeTimer 渲染每日自动升级单元。
func RenderUpgradeService(cli, root string) (string, error) {
	return render("upgrade-service.tpl", map[string]string{
		"Cli": cli, "Root": root,
		"Prog": constants.ProgName, "EnvPrefix": constants.EnvPrefix(),
	})
}
func RenderUpgradeTimer() (string, error) {
	return render("upgrade-timer.tpl", map[string]string{
		"Prog": constants.ProgName, "EnvPrefix": constants.EnvPrefix(),
	})
}

// ConfigData 渲染 mihomo 配置所需字段。
type ConfigData struct {
	Prog        string // 程序名(渲染到配置头部注释,随编译期 ProgName 注入)
	MixedPort   int
	ApiPort     int
	Secret      string
	TProxy      bool // true=TPROXY 变体(无 tun 段,加 tproxy-port)
	TproxyPort  int
	DnsPort     int
	RoutingMark int
}

// DefaultConfigData 常用默认值。
func DefaultConfigData() ConfigData {
	return ConfigData{
		Prog:        constants.ProgName,
		Secret:      constants.DefSecret,
		MixedPort:   constants.MixedPortDef,
		ApiPort:     constants.ApiPortDef,
		TProxy:      false,
		TproxyPort:  constants.TproxyPort,
		DnsPort:     constants.DnsListenPort,
		RoutingMark: constants.MarkSelf,
	}
}

// RenderConfig 渲染完整 mihomo 配置(含全部默认分组/规则,承接 bash 版 v0.1.4 资产)。
func RenderConfig(d ConfigData) (string, error) { return render("config.tpl", d) }

// TunParams 与 TunRouteExclude 是 TUN 模式配置块的唯一事实源:config.tpl 模板渲染、
// config.SetMode 增量重建都从这里取数,避免两处硬编码漂移(改动务必在此,模板随之同步)。
var (
	TunParams = [][2]string{
		{"enable", "true"},
		{"stack", "system"},
		{"auto-route", "true"},
		{"auto-detect-interface", "true"},
		{"strict-route", "true"},
		{"mtu", "1500"},
	}
	TunRouteExclude = []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
)
