## 故障排查

- `status` 节点=0:订阅没加载 → 重跑 `sub import`(可 `--file` 离线),仍失败 `Panoxy log`
- 断流先 `systemctl restart Panoxy`(防火墙自愈);持续则 `Panoxy mode` 确认模式、`--debug` 看规则加载
- 配置改坏:`Panoxy check` + 内核报错会透传首条 `level=error msg`
- 升级异常:`Panoxy rollback`;`.last-upgrade` 过旧=升级停滞,查 `Panoxy log`
