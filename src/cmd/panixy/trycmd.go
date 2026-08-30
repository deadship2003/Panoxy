package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/logx"
)

// runTry 预安装(免 root 沙箱实测):在不触碰真实系统的前提下,把 init/deploy 的
// 全流程真跑一遍 —— 真实下载资产、真实启动内核(TUN/routing-mark 按非 root 约束
// 剥离)、真实导入订阅并验证节点数、真实健康检查。通过 = 可以放心 sudo 真装。
//
// 实现即"产品化的 e2e 沙箱":路径全部经环境变量重定向到沙箱目录;systemd/ip/
// sysctl 用内置 shim 替身(内核引导时剥 tun 与 routing-mark —— 非 root 下
// TUN 建设备与 SO_MARK 会 EPERM,真机 root 部署无此限制)。
func runTry(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("panixy-try-%d", time.Now().Unix()))
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		return err
	}

	// 沙箱 systemd 替身:enable/restart 以剥离 tun+routing-mark 的配置直接引导内核
	shim := filepath.Join(dir, "bin", "systemctl")
	pidf := filepath.Join(dir, "pid")
	// 退出时兜底停掉沙箱内核:不留后台 mihomo 占用透明代理端口,
	// 否则紧接着的 sudo panixy init/deploy 会被自己的残留进程挡住(端口占用)。
	defer stopSandboxMihomo(pidf)
	shimScript := fmt.Sprintf(`#!/bin/sh
# {{PROG}} try 沙箱替身(产品化 e2e):非 root 引导内核时剥离 tun 段与 routing-mark
# (TUN 建设备/SO_MARK 需 CAP_NET_ADMIN;真机 root 部署不受此限制)
PIDF=%s
start_mh() {
  awk '/^tun:/{s=1;next} /^routing-mark:/{next} s && /^[^ \t#]/{s=0} !s{print}' "${{PREFIX}}_CONF" > "${{PREFIX}}_CONF.notun"
  "${{PREFIX}}_ROOT/bin/mihomo" -f "${{PREFIX}}_CONF.notun" -d "${{PREFIX}}_ROOT" >> "${{PREFIX}}_ROOT/run.log" 2>&1 < /dev/null &
  echo $! > "$PIDF"
}
case "$1" in
  restart|disable) while read p; do kill "$p" 2>/dev/null; done < "$PIDF" 2>/dev/null; : > "$PIDF"
    [ "$1" = restart ] && { sleep 1; start_mh; } ;;
  enable) [ "$2" = "--now" ] && [ "$3" = {{PROG}}.service ] && start_mh ;;
  is-active) alive=0; while read p; do kill -0 "$p" 2>/dev/null && alive=1; done < "$PIDF" 2>/dev/null
    [ "$alive" = 1 ] && echo active || { echo inactive; exit 3; } ;;
esac
exit 0
`, pidf)
	shimScript = strings.ReplaceAll(shimScript, "{{PROG}}", constants.ProgName)
	shimScript = strings.ReplaceAll(shimScript, "{{PREFIX}}", constants.EnvPrefix())
	if err := os.WriteFile(shim, []byte(shimScript), 0o755); err != nil {
		return err
	}
	for _, name := range []string{"ip", "sysctl"} {
		os.WriteFile(filepath.Join(dir, "bin", name), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}

	// 路径全部重定向进沙箱;PATH 前置 shim;免 root
	pfx := constants.EnvPrefix()
	for k, v := range map[string]string{
		pfx + "_ROOT":          filepath.Join(dir, "root"),
		pfx + "_CONF":          filepath.Join(dir, "clash.yaml"),
		pfx + "_UNIT_DIR":      filepath.Join(dir, "units"),
		pfx + "_CLI":           filepath.Join(dir, "cli", constants.ProgName),
		pfx + "_MAN":           filepath.Join(dir, "man", constants.ProgName+".1.gz"),
		pfx + "_STATE":         filepath.Join(dir, "state.yaml"),
		pfx + "_SYSCTL":        filepath.Join(dir, "99-sysctl.conf"),
		pfx + "_LOCK":          filepath.Join(dir, "lock"),
		pfx + "_ALLOW_NONROOT": "1",
		"PATH":                 filepath.Join(dir, "bin") + ":" + os.Getenv("PATH"),
	} {
		os.Setenv(k, v)
	}
	// 复用环境:之后可在同 shell 对沙箱跑 status/sub list 等
	envFile := filepath.Join(dir, "env.sh")
	os.WriteFile(envFile, []byte(fmt.Sprintf(`# source 后即可对本沙箱执行 %[1]s status / sub list / sub import 等
export %[2]s_ROOT=%[3]q
export %[2]s_CONF=%[4]q
export %[2]s_UNIT_DIR=%[5]q
export %[2]s_CLI=%[6]q
export %[2]s_MAN=%[7]q
export %[2]s_STATE=%[8]q
export %[2]s_SYSCTL=%[9]q
export %[2]s_LOCK=%[10]q
export %[2]s_ALLOW_NONROOT=1
# 沙箱替身优先(status/is-active 等对沙箱生效)
case ":$PATH:" in
  *":%[11]s:"*) ;;
  *) export PATH="%[11]s:$PATH" ;;
esac
`,
		constants.ProgName,
		pfx, filepath.Join(dir, "root"), filepath.Join(dir, "clash.yaml"), filepath.Join(dir, "units"),
		filepath.Join(dir, "cli", constants.ProgName), filepath.Join(dir, "man", constants.ProgName+".1.gz"),
		filepath.Join(dir, "state.yaml"), filepath.Join(dir, "99-sysctl.conf"), filepath.Join(dir, "lock"),
		filepath.Join(dir, "bin"))), 0o644)

	logx.Info("沙箱:%s(免 root;真实下载/内核/订阅,防火墙与 TUN 不落地)", dir)
	logx.Info("沙箱约束:引导时剥离 tun 与 routing-mark(非 root 限制);真实部署(sudo init)无此限制")
	logx.Info("提示:若本机已有部署占用 33833/9999/1053 端口,先停服务或换机器 try")

	// 复用 init 全流程(其内部 needRoot 已被 PANIXY_ALLOW_NONROOT 放行)
	if err := runInit(cmd, args); err != nil {
		return fmt.Errorf("预安装未通过:%w\n沙箱保留在 %s(查 %s/root/run.log),排除后重试或 rm -rf 清理", err, dir, dir)
	}

	fmt.Fprintln(os.Stderr)
	logx.Info("预安装通过 ✓ 真实部署请执行: sudo %s init %s", constants.ProgName, subArgsHint(args))
	logx.Info("沙箱内核已停止(不留后台进程,不影响后续 deploy/init);沙箱文件保留在 %s(查 %s/root/run.log)", dir, dir)
	logx.Info("清理沙箱: rm -rf %s   # 随时可删,不影响系统", dir)
	// 确保终端干净返回 prompt(stdout+stderr 各补一个换行并 flush)
	fmt.Fprintln(os.Stdout)
	os.Stdout.Sync()
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
	return nil
}

func subArgsHint(args []string) string {
	if len(args) > 0 {
		return "'" + args[0] + "'   # 记得引号"
	}
	return "(回车粘贴订阅)"
}

// stopSandboxMihomo 停止沙箱替身启动的内核(shim 的 start_mh 把 pid 写入 pidf)。
// try 结束后必须调用,否则残留 mihomo 会占用透明代理端口,阻塞后续真实 init/deploy。
func stopSandboxMihomo(pidf string) {
	b, err := os.ReadFile(pidf)
	if err != nil {
		return
	}
	for _, p := range strings.Fields(string(b)) {
		syscallKill(p)
	}
}
