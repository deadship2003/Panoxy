## 从 bash 版迁移与升级

panoxy 是 bash 版的 Go 重写,分两种操作:**全新迁移**(bash → panoxy,一次性)
与**就地升级**(panoxy → 新版本,保留配置与订阅)。迁移不做自动转换:检测到
bash 残留会中止并提示,由用户手工清理。

### 一、从 bash 版全新迁移

bash 版残留特征:systemd 单元含 `resolvectl`、旧配置 `/etc/clash.yaml` 含
`tun.dns-hijack` 段。

1. 停服并清单元:`sudo panoxy uninstall`(停服务、清防火墙、删单元/sysctl/man;
   保留 `/opt/panoxy` 数据与 `/etc/panoxy.yaml` 配置)
2. 删除或清空旧配置 `/etc/clash.yaml`;想保留分组可手工去掉 `tun.dns-hijack` 段
3. 部署:离线包 `sudo ./panoxy deploy`(或裸机联网 `sudo panoxy init`)
4. 导入订阅:`sudo panoxy sub import`

> 护栏:deploy/init 检测到 bash 残留(单元含 resolvectl / 配置含 dns-hijack)
> 会主动中止并提示,清干净后重试即可(`panoxy deploy --dry-run` 可先预检)。

### 二、就地升级(保留配置与订阅)

内核已内嵌进 CLI,大多数升级只换二进制即可:

- 方式 A(推荐,顺带刷新单元/man/sysctl/默认配置基线):
  `sudo panoxy stop` → 用**新编译的二进制** `sudo ./dist/panoxy redeploy`
  (redeploy 会把当前运行的二进制复制到 `/usr/local/bin/panoxy`)
- 方式 B(只换二进制):`cp dist/panoxy /usr/local/bin/panoxy` → `sudo panoxy restart`
