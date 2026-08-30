package firewall

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/logx"
)

// ---- TPROXY 策略路由(fwmark → table → local 路由;v4/v6 各一套)----
// 规则落在主命名空间(无 netns),Teardown/CleanAll 幂等清理。

// TproxyPolicyAdd 加载策略路由(TPROXY 模式 Apply 时调用;先幂等清旧再加载,防累积)。
func TproxyPolicyAdd() error {
	return runIps(tproxyPolicyCmds(true, constants.MarkTproxy, constants.TproxyTable), "load TPROXY policy routing")
}

// TproxyPolicyDel 清理策略路由(幂等)。
func TproxyPolicyDel() error {
	return runIps(tproxyPolicyCmds(false, constants.MarkTproxy, constants.TproxyTable), "clean TPROXY policy routing")
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
		// 幂等容错:清理时"不存在"、加载时"已存在"都视为成功。
		// 注意 ip 对不存在的 rule/table 报 "No such file or directory"(RTNETLINK
		// ENOENT)—— 真机首装必现,漏容会导致 fw apply 失败、服务被判死(实测踩过)。
		if err != nil && !tolerantError(string(out)) {
			return fmt.Errorf("%s failed: %s", what, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// tolerantError 判定可忽略的幂等性错误输出。
func tolerantError(out string) bool {
	for _, s := range []string{
		"File exists",               // add 时已存在
		"No such process",           // del 部分实现
		"No such file or directory", // rule/table 不存在(RTNETLINK ENOENT)
		"does not exist",            // 新版 iproute2: FIB rule does not exist
		"Cannot find device",
	} {
		if strings.Contains(out, s) {
			return true
		}
	}
	return false
}
