package main

import (
	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/core"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/metacubex/mihomo/hub"
)

// cmdRun 是进程内内核入口,由 systemd 单元的 ExecStart 调用;阻塞至 SIGTERM。
// 配置与 geodata 都从安装目录读取;其余子命令在独立进程中通过 REST API 管理本守护进程。
func cmdRun() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "run the embedded mihomo kernel in-process (systemd ExecStart; not for direct use)",
		Long: `Run the embedded mihomo kernel in-process, reading the config and geodata from the install
directory. This is the ExecStart of the panixy systemd unit; it blocks until SIGTERM.

Management is done by the other panixy subcommands (sub/mode/etc.) in separate processes, which
talk to this daemon over the REST API. Normally you never run this directly.`,
		RunE: runKernel,
	}
}

func runKernel(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	return core.Run(p.Root, p.Conf, hub.WithExternalUI(p.UiDir))
}
