// Package statemode 读写 Panoxy 自身状态文件(/opt/Panoxy/Panoxy.yaml):
// proxy-mode 等由 CLI 程序化管理的设置。用户不需要手编;缺省 tun。
package statemode

import (
	"os"

	"gopkg.in/yaml.v3"
)

type State struct {
	ProxyMode string `yaml:"proxy-mode"` // tun | tproxy
}

// Read 读取状态;文件缺失/损坏一律返回默认值(缺省 tun),不报错(读路径不挡道)。
func Read(path string) string {
	return normalize(readState(path).ProxyMode)
}

// readState 返回完整状态结构。
func readState(path string) State {
	var st State
	b, err := os.ReadFile(path)
	if err != nil {
		return State{ProxyMode: "tun"}
	}
	if err := yaml.Unmarshal(b, &st); err != nil {
		return State{ProxyMode: "tun"}
	}
	if st.ProxyMode == "" {
		st.ProxyMode = "tun"
	}
	return st
}

// Write 原子写入状态。
func Write(path string, st State) error {
	st.ProxyMode = normalize(st.ProxyMode)
	b, err := yaml.Marshal(&st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalize(m string) string {
	if m == "tproxy" {
		return "tproxy"
	}
	return "tun"
}
