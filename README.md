# Panixy(Go 版)

> 基于 [mihomo](https://github.com/MetaCubeX/mihomo) 内核的 Linux 透明代理网关部署/管理工具。
> 单二进制(CGO=0,amd64/arm64);**管理工具,不实现任何转发逻辑** —— 流量与 DNS 全部由 mihomo 完成。

取代 bash 版:模板渲染、增量配置编辑、防火墙 DNS 劫持、订阅管理、升级回滚、健康检测,事务式 + 全量回滚。

## ✨ 特性

- **TUN / TPROXY 双模式**(默认 tun):TUN 开箱稳定;TPROXY 保留客户端真实源 IP、内核转发性能最优
- **DNS 劫持 = nftables(降级 iptables)**:独立表 `inet panixy`,53 redirect → mihomo:1053,拒绝 853(DoT/DoQ);mihomo 自身流量按 `routing-mark 6666` 放行防回环
- **自愈**:服务 `ExecStartPost` 调 `panixy fw apply`(先无条件清表再加载)—— **kill -9/OOM 残留随 `systemctl restart panixy` 自动清除**,正常 stop 由 `ExecStop` 清理
- **订阅可验证**:预取(直连→本机代理/`--file` 离线导入)→ 校验 → 增量写入 provider(锚点复用、保注释)→ 重启 → **节点数>0 才算成功**;`--name` 多订阅并存,自动融合进策略组
- **完整模板资产**:bash 版 v0.1.4 全量分组/规则移植(地区组/应用组/GEO 规则/广告拦截),set-sub 只动 provider 与 use 列表
- **升级参数化**:`upgrade --core/--ui/--check/--core-version`;试运行校验、双健康检查、失败自动回滚、备份 KEEP=3
- **全量文档**:`-h/-?/--help` 每命令含示例;`man panixy` 与 `--help` 同源生成(cobra),永不漂移
- **调试友好**:`--verbose` 分步明细;`--debug` 外部命令/API I/O 零遮蔽(吸取 bash 版吞 stderr 教训)

## 🚀 快速开始

从 [Releases](../../releases) 下载对应架构离线包(amd64/arm64,含内核+geo+UI+规则):

```bash
tar xzf Panixy-V0.1.0-amd64.tar.gz && cd Panixy-V0.1.0-amd64
sudo ./panixy deploy                 # 全新部署(资产/配置/CLI/手册/服务/防火墙)
sudo panixy set-sub                  # 回车进入粘贴模式,粘订阅链接,无需引号
panixy status                        # 节点/服务/防火墙/出口 一览
```

一条命令到位:`sudo ./panixy deploy '订阅链接'`。无外网导入:`sudo panixy set-sub --file 订阅.yaml`。

## 架构

```
                 DNS(53/853)                        数据流量(非53)
┌──────────┐  nft redirect → :1053  ┌─┐  路由表 → TUN 设备 → mihomo(system 栈)
│ TUN 模式 │ ─────────────────────► │同│
├──────────┤                        │一│  nft mark 1 + 策略路由(table 100)
│TPROXY模式│  nft redirect → :1053  │套│  + tproxy → :7893(保留客户端源 IP)
└──────────┘ ─────────────────────► └─┘
```

- 排除项共用:保留网段/回环不劫持;mihomo 自身出站(`routing-mark: 6666`)放行 —— 防 DNS 回环死锁
- 模式切换:**数据面(节点/组)在 Web 面板;传输面(tun/tproxy)必须走 `panixy mode`**(防火墙与配置需同事务变更,面板做不到)
- systemd 单元零 resolvectl;`fw apply` 自清洁实现 restart 自愈

## 目录布局

```
/opt/panixy/            # 数据家目录:bin/mihomo、ui/official、proxies/(订阅缓存)、
                        # rule_provider/、geo、panixy.yaml(状态:proxy-mode)、.last-upgrade
/etc/clash.yaml         # mihomo 配置(管理员可手编;唯一事实源)
/usr/local/bin/panixy   # CLI         /usr/local/share/man/man1/panixy.1.gz  # 手册
```

## 命令

| 命令 | 作用 |
|---|---|
| `sudo ./panixy deploy [URL]` | 全新部署(离线包内运行;`--proxy-mode tproxy`;失败全量回滚;检测 bash 旧版残留并中止) |
| `sudo panixy install` | 仅部署服务/防火墙(文件已就位) |
| `sudo panixy set-sub [URL]` | 导入/更换订阅(`--name/--file/--group`;粘贴模式免引号;节点数>0 才成功) |
| `sudo panixy sub-rm --name N` | 删除订阅(对称反融合;删光唯一订阅会被 `-t` 拒绝并回滚) |
| `panixy sub-list [--json]` | 各订阅状态/节点数(✅/⚠️获取失败/⚠️解析失败/⚠️节点0) |
| `panixy status [-v\|-q\|--json]` | 健康一览;`-q` 退出码 0健康/1降级/2故障(监控用) |
| `sudo panixy mode [tun\|tproxy]` | 查看/原子切换模式(tproxy 需内核 xt_TPROXY) |
| `sudo panixy upgrade [--core\|--ui] [--check] [--core-version vX]` | 参数化升级(timer 每日 04:17 自动) |
| `sudo panixy rollback [vX.Y.Z]` | 内核回滚(默认最近备份) |
| `panixy check [yaml]` / `sudo panixy apply-conf <yaml>` | 校验 / 应用配置(热重载优先;**热重载不刷新 provider**) |
| `sudo panixy uninstall` | 停服务+清防火墙+删单元(**保留 /opt 数据与配置**) |
| `panixy units` / `panixy log [n]` / `panixy man` / `panixy fw <apply\|teardown\|clean>` | 单元审查 / 日志 / 手册 / 防火墙管理 |

TUN vs TPROXY 选型:家用、要稳、少折腾 → **tun**(默认);弱 ARM 跑满千兆、日志必须看到设备真实 IP → tproxy(注意 IPv6 策略路由与容器误劫持坑)。

## 构建与打包

```bash
scripts/build.sh                 # 双架构静态二进制 → dist/
scripts/package.sh --arch all    # 编译+下载内核/geo(含Country.mmdb)/UI/规则+泄露扫描 → 离线包
```

CI(推 `V*` 标签)与本地同一脚本。**订阅 URL 永不进包**:占位符 + 打包前泄露扫描(`token=` 等特征命中即中止)。

## 从 bash 版迁移(手动,不做自动转换)

1. 旧机器:`sudo panixy uninstall`(停服务/清单元;`systemctl revert` 恢复 resolved 若曾被接管)
2. 删除或清空 `/etc/clash.yaml`(含 `dns-hijack` 旧配置;想保留分组可手工去 tun.dns-hijack 段)
3. 新包 `sudo ./panixy deploy` → `set-sub` 导入订阅
4. 新 deploy 检测到旧特征(unit 含 resolvectl/配置含 dns-hijack)会主动中止并提示

## 已知限制(必读)

1. **热重载不刷新 proxy-providers**(mihomo 限制):set-sub/sub-rm/mode 一律重启进程生效
2. kill -9/OOM 会残留防火墙规则:`systemctl restart panixy` 启动即自动清理,无需手工
3. **DoH(443)无法在内核劫持**:浏览器内置加密 DNS 不走分流,status 已提示,建议关闭
4. 订阅预取只是预校验;运行期 mihomo 会按 interval 自行远程拉取
5. set-sub `--name` 依赖配置锚点 `&p`(基础模板自带;纯手写配置需自备)
6. tun `stack: system` 家用默认;重度 BT/长时 UDP 流媒体/节点频繁掉线/老内核(5.4/5.15)建议改 `gvisor`(进程崩溃可被 systemd 自动拉起,优于静默僵死)

## 开发

```bash
go test ./internal/...    # 单测(模板过真实 mihomo -t、配置编辑器黄金样例、防火墙规则文本)
go test ./tests/          # e2e(真实内核+假 systemd 沙箱:deploy/set-sub/mode 全事务链)
MIHOMO_BIN=/path/to/mihomo GEO_SRC=/path/to/geo go test ./...   # 指定本机内核与 geo
```

## 故障排查

- `status` 节点=0:订阅没加载 → 重跑 `set-sub`(可 `--file` 离线),仍失败 `panixy log`
- 断流先 `systemctl restart panixy`(防火墙自愈);持续则 `panixy mode` 确认模式、`--debug` 看规则加载
- 配置改坏:`panixy check` + 内核报错会透传首条 `level=error msg`
- 升级异常:`panixy rollback`;`.last-upgrade` 过旧=升级停滞,查 `panixy log`
