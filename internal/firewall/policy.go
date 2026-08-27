package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/deadship2003/panixy/internal/constants"
	"github.com/deadship2003/panixy/internal/logx"
)

// ---- TPROXY 策略路由(fwmark → table → local 路由;v4/v6 各一套)----
// 规则落在主命名空间(无 netns),Teardown/CleanAll 幂等清理。

// TproxyPolicyAdd 加载策略路由(TPROXY 模式 Apply 时调用;先幂等清旧再加载,防累积)。
func TproxyPolicyAdd() error {
	return runIps(tproxyPolicyCmds(true, constants.MarkTproxy, constants.TproxyTable), "加载 TPROXY 策略路由")
}

// TproxyPolicyDel 清理策略路由(幂等)。
func TproxyPolicyDel() error {
	return runIps(tproxyPolicyCmds(false, constants.MarkTproxy, constants.TproxyTable), "清理 TPROXY 策略路由")
}

func tproxyPolicyCmds(add bool, mark, table int) [][]string {
	t := fmt.Sprint(table)
	m := fmt.Sprint(mark)
	act := "del"
	if add {
		act = "add"
	}
	flush := "flush"
	return [][]string{
		{"ip", "rule", act, "fwmark", m, "lookup", t},
		{"ip", "route", flush, "table", t},
		{"ip", "-6", "rule", act, "fwmark", m, "lookup", t},
		{"ip", "-6", "route", flush, "table", t},
		{"ip", "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", t},
		{"ip", "-6", "route", "add", "local", "::/0", "dev", "lo", "table", t},
	}
}

func runIps(cmds [][]string, what string) error {
	for _, c := range cmds {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		logx.DebugCmd(c[0], c[1:], string(out), err)
		// 幂等:不存在(清理)/已存在(加载)都视为成功
		if err != nil && !strings.Contains(string(out), "File exists") &&
			!strings.Contains(string(out), "No such process") {
			return fmt.Errorf("%s 失败: %s", what, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
