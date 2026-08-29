## 从 bash 版迁移(手动,不做自动转换)

1. 旧机器:`sudo panixy uninstall`(停服务/清单元;`systemctl revert` 恢复 resolved 若曾被接管)
2. 删除或清空 `/etc/clash.yaml`(含 `dns-hijack` 旧配置;想保留分组可手工去 tun.dns-hijack 段)
3. 新包 `sudo ./panixy deploy` → `sub import` 导入订阅
4. 新 deploy 检测到旧特征(unit 含 resolvectl/配置含 dns-hijack)会主动中止并提示
