// Package logx 分级日志:默认 INFO(一行一步,延续 bash 版节奏);--verbose 显示分步明细;
// --debug 全量透传(外部命令原样回显、mihomo API 请求响应、文件写入)。
// 所有日志走 stderr,stdout 保留给 --json 等机器输出。
//
// 教训背景:bash 版曾因 exec 重定向吞掉整个脚本 stderr、内核日志走 stdout 被
// >/dev/null 吃掉,排查"静默失败"花费整轮 —— Go 版外部调用 I/O 在 debug 级零遮蔽。
package logx

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deadship2003/panoxy/internal/constants"
)

const (
	LevelInfo = iota
	LevelVerbose
	LevelDebug
)

var level = LevelInfo

// SetLevel 由 cobra persistent flags 注入。
func SetLevel(l int) { level = l }

func stamp(lv string, msg string) string {
	return fmt.Sprintf("[%s] %s %-7s %s", constants.ProgName, time.Now().Format("2006-01-02 15:04:05"), lv, msg)
}

func Info(format string, a ...any) {
	fmt.Fprintln(os.Stderr, stamp("INFO", fmt.Sprintf(format, a...)))
}

// Step 是 --verbose 级:事务分步(如 "[3/7] 写入配置(备份→.panoxy-bak,即 BackupSuffix 派生)")。
func Step(format string, a ...any) {
	if level >= LevelVerbose {
		fmt.Fprintln(os.Stderr, stamp("STEP", fmt.Sprintf(format, a...)))
	}
}

// Debug 是全量级:外部命令/API 的输入输出原样回显。
func Debug(format string, a ...any) {
	if level >= LevelDebug {
		fmt.Fprintln(os.Stderr, stamp("DEBUG", fmt.Sprintf(format, a...)))
	}
}

// DebugCmd 回显一条外部命令的完整命令行与输出(multiline 原样保留)。
func DebugCmd(cmd string, args []string, out string, err error) {
	if level < LevelDebug {
		return
	}
	c := strings.TrimSpace(cmd + " " + strings.Join(args, " "))
	o := strings.TrimRight(out, "\n")
	line := "$ " + c
	if o != "" {
		line += "\n" + indent(o)
	}
	if err != nil {
		line += "\n  (exit: " + err.Error() + ")"
	}
	fmt.Fprintln(os.Stderr, stamp("CMD", line))
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// Warn/Error 走 stderr 且始终显示。
func Warn(format string, a ...any) {
	fmt.Fprintln(os.Stderr, stamp("WARN", fmt.Sprintf(format, a...)))
}

func Error(format string, a ...any) {
	fmt.Fprintln(os.Stderr, stamp("ERROR", fmt.Sprintf(format, a...)))
}
