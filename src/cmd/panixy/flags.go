package main

import (
	"github.com/spf13/cobra"

	"github.com/deadship2003/panixy/internal/constants"
)

// 订阅与部署参数的统一注册:init/deploy/try/sub import 共享同一套 flag,
// 保证默认值与描述一致,避免日后只改一处造成漂移(单一事实源)。

// addSubSourceFlags 订阅来源:--name/--file。
func addSubSourceFlags(c *cobra.Command) {
	c.Flags().String("name", "SUB", "订阅 provider 名称(仅 [a-zA-Z0-9_-])")
	c.Flags().String("file", "", "本地订阅 YAML 文件(跳过联网拉取)")
}

// addDeployFlags 部署参数:--proxy-mode/--secret。
func addDeployFlags(c *cobra.Command) {
	c.Flags().String("proxy-mode", "tun", "透明代理模式: tun | tproxy")
	c.Flags().String("secret", constants.DefSecret, "面板/API 密钥")
}

// addDownloadFlags 下载兜底参数:--mirror/--boot-bin(init/try 需联网下载共用)。
func addDownloadFlags(c *cobra.Command) {
	c.Flags().StringSlice("mirror", nil, "gh 镜像前缀(可多个;第三方源,内核经试运行校验)")
	c.Flags().String("boot-bin", "", "订阅引导代理所用内核(默认取安装目录下 bin/mihomo)")
}

// addDryRunFlag 试运行参数(各命令语义略异,由 desc 描述)。
func addDryRunFlag(c *cobra.Command, desc string) {
	c.Flags().Bool("dry-run", false, desc)
}
