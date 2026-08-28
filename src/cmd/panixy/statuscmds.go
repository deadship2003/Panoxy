package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/health"
	"github.com/deadship2003/panixy/internal/paths"
)

func runStatus(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	v, _ := cmd.Flags().GetBool("verbose")
	q, _ := cmd.Flags().GetBool("quiet")
	asJSON, _ := cmd.Flags().GetBool("json")

	r := health.Collect(p.Conf, p.Bin, p.UiStamp, p.LastUp, p.State)
	// 修正残留规则判定:表存在+服务 active=正常;表存在+服务 inactive=真残留
	if r.Stale && r.Service == "active" {
		r.Stale = false // 服务在跑,表里有规则是正常状态
	}

	// -q:仅退出码(0健康 1降级 2故障)
	if q {
		if r.Service != "active" || !r.APIAlive {
			os.Exit(2)
		}
		if r.Nodes == 0 || r.Egress != "204" {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if asJSON {
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("== panixy v%s  (%s) ==\n", constants.Version, p.Root)
	fmt.Printf("服务:     %s\n", r.Service)
	fmt.Printf("模式:     %s   防火墙: %s", r.Mode, r.FwBackend)
	if r.Stale {
		fmt.Print("  ⚠️ 检测到残留规则(restart panixy 自愈)")
	}
	fmt.Println()
	for _, st := range r.Providers {
		mark := "✅"
		err := ""
		if st.Error != "" {
			mark, err = "⚠️", "  "+st.Error
		} else if st.Nodes == 0 {
			mark = "⚠️ 节点为0"
		}
		fmt.Printf("订阅:     %s %-16s %d 节点%s\n", mark, st.Name, st.Nodes, err)
	}
	if len(r.Providers) == 0 {
		fmt.Printf("订阅:     -(配置无 proxy-providers)\n")
	}
	fmt.Printf("内核:     %s\n", r.CoreVer)
	fmt.Printf("UI:       %s   上次升级: %s\n", r.UIVer, orUnknown(r.LastUp))
	fmt.Printf("API:      %s\n", orUnreachable(r.APIAlive, r.APIVer))
	fmt.Printf("代理出网: %s (期望204)\n", r.Egress)
	fmt.Printf("直连出网: %s (期望204)\n", r.Direct)
	fmt.Println("提示:     浏览器内置 DoH(443)无法被内核劫持,域名分流对其不生效,建议关闭")

	if v {
		fmt.Println("-- 详细(-v) --")
		if r.Mode == "tun" {
			fmt.Println("TUN 栈:   system(当前配置;重度 BT/UDP、频繁掉线、老内核建议 gvisor 兜底)")
		} else {
			fmt.Println("TPROXY:   保留客户端源 IP;注意 IPv6/容器劫持坑(见 README)")
		}
		if b, err := os.ReadFile(p.State); err == nil {
			fmt.Printf("状态文件: %s\n%s", p.State, indentLines(string(b)))
		}
		fmt.Println("数据面(节点/组选择)请在 Web 面板操作;传输面(tun/tproxy)用 panixy mode")
	}
	return nil
}

func runMode(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		p := paths.Get()
		fmt.Println(healthReadMode(p.State))
		return nil
	}
	return modeSwitch(args[0])
}

func orUnknown(s string) string {
	if s == "" {
		return "未知"
	}
	return s
}
func orUnreachable(ok bool, v string) string {
	if !ok {
		return "不可达"
	}
	return v
}
func indentLines(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n  ")
}
