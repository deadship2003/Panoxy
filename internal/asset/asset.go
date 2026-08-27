// Package asset 内嵌资源:systemd 单元模板与 mihomo 完整配置模板(tun/tproxy 双模式变体)。
package asset

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/deadship2003/panixy/internal/constants"
)

//go:embed service.tpl upgrade-service.tpl upgrade-timer.tpl config.tpl
var files embed.FS

// UnitData 渲染主服务单元所需字段。
type UnitData struct {
	Mode string // tun / tproxy(仅用于 Description)
	Bin, Conf, Root, UiDir, Cli string
}

func render(name string, data any) (string, error) {
	raw, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("内嵌资源缺失: %s: %w", name, err)
	}
	t, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("模板 %s 解析失败: %w", name, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("模板 %s 渲染失败: %w", name, err)
	}
	return b.String(), nil
}

// RenderService 渲染 panixy.service(无任何 resolvectl 逻辑;
// fw apply 内部先无条件 CleanAll,kill -9 残留随 restart 自愈)。
func RenderService(d UnitData) (string, error) { return render("service.tpl", d) }

// RenderUpgradeService / RenderUpgradeTimer 渲染每日自动升级单元。
func RenderUpgradeService(cli string) (string, error) {
	return render("upgrade-service.tpl", map[string]string{"Cli": cli})
}
func RenderUpgradeTimer() (string, error) { return render("upgrade-timer.tpl", nil) }

// ConfigData 渲染 mihomo 配置所需字段。
type ConfigData struct {
	MixedPort  int
	ApiPort    int
	Secret     string
	TProxy     bool // true=TPROXY 变体(无 tun 段,加 tproxy-port)
	TproxyPort int
	DnsPort    int
	RoutingMark int
}

// DefaultConfigData 常用默认值。
func DefaultConfigData() ConfigData {
	return ConfigData{
		MixedPort:  constants.MixedPortDef,
		ApiPort:    constants.ApiPortDef,
		TProxy:     false,
		TproxyPort: constants.TproxyPort,
		DnsPort:    constants.DnsListenPort,
		RoutingMark: constants.MarkSelf,
	}
}

// RenderConfig 渲染完整 mihomo 配置(含全部默认分组/规则,承接 bash 版 v0.1.4 资产)。
func RenderConfig(d ConfigData) (string, error) { return render("config.tpl", d) }
