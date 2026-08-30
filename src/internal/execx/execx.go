// Package execx 统一外部命令执行:CombinedOutput + debug 级原样回显(零遮蔽)。
package execx

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/deadship2003/Panoxy/internal/logx"
)

// Run 执行并返回合并输出;失败时错误信息附带输出(教训:mihomo 日志走 stdout,
// 只看 stderr 会"静默失败")。
func Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	logx.DebugCmd(name, args, string(out), err)
	return string(out), err
}

// RunOK 要求成功,失败返回带上下文的错误。
func RunOK(what, name string, args ...string) (string, error) {
	out, err := Run(name, args...)
	if err != nil {
		return out, fmt.Errorf("%s failed: %s", what, strings.TrimSpace(out))
	}
	return out, nil
}

// RunShell 执行 shell 行(用于含 || 的幂等命令串;仅限常量生成的命令,无注入面)。
func RunShell(line string) (string, error) {
	out, err := exec.Command("sh", "-c", line).CombinedOutput()
	logx.DebugCmd("sh", []string{"-c", line}, string(out), err)
	return string(out), err
}
