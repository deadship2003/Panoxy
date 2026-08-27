# Panixy

> 基于 [mihomo](https://github.com/MetaCubeX/mihomo) 内核的 Linux 透明代理一键部署套件(V0.0.1)

取代已归档的 TPClash:进程守护、systemd-resolved 接管、**内核与 Web 面板每日自动升级(带健康检查与自动回滚)**。面向网关全新部署,一条命令落地。

## ✨ 特性

- **全离线安装**——内核(amd64-V3/arm64 双架构)、Geo 数据、metacubexd 面板、广告分流规则、配置模板全在包内,装完才需要网
- **订阅导入可验证**——`set-sub` 直连→经本机代理两级预取订阅(或直接给本地文件离线导入),预置缓存后重启内核,**以节点数 > 0 为成功标准**,绝不假成功
- **事务式安装**——任何一步失败自动回滚到运行前状态,不留残迹
- **每日自动升级**——内核与面板经本机代理下载、试运行校验、原子替换、双健康检查(API 版本 + 经代理出网 204),失败自动回退,内核保留 3 份备份
- **热重载生效**——`apply-conf` 优先 API 热重载,不重启、不重建 TUN,网关链路无损(`set-sub` 换 URL 必须重启重建 provider,见下节)
- **安全默认**——API 密钥安装时随机生成;联动参数(密钥/端口)运行时从配置解析,配置是唯一事实源
- **并发保护**——flock 互斥,自动升级与手工操作不互踩
- **通用模板**——全文件只需改一处订阅链接;国内流量默认 DIRECT,地区分组按常见机场命名自动归类

## 🚀 快速开始(就两步)

从 [Releases](../../releases) 下载 `Panixy-V0.0.1.tar.gz` 后:

```bash
tar xzf Panixy-V0.0.1.tar.gz && cd Panixy-V0.0.1
sudo ./install.sh                       # 全离线安装
sudo panixy set-sub '你的订阅链接'       # 用模板时唯一必改项
```

也可以把订阅链接直接传给安装器一步到位:`sudo ./install.sh 'https://你的订阅链接'`

**无外网环境**(订阅域名直连不可达、本机也没有可用节点)时离线导入:在任意能访问订阅的设备上下载好
Clash 订阅 YAML,拷到网关后:

```bash
sudo panixy set-sub 'https://你的订阅链接' /path/to/订阅.yaml
```

导入后内核从预置缓存**秒级加载节点,不依赖当时的网络**;测速选优由 `🔃 自动选择` 组自动进行,各策略组默认走最快节点,境外域名的真实 DNS 解析也经 `dns` 组(默认同样是最快节点)走 DoH。

安装是事务式的:任何一步失败(内核缺失/配置校验不过/服务起不来/健康验证超时),自动停用并移除 unit/timer、还原 sysctl 与 ip_forward、删除本次新建的目录与文件,系统回到运行前状态。

配置模板 `/etc/clash.yaml` 已内置全套常见分组(自动选择/地区组/ChatGPT/油管/网飞/Telegram/Twitter/微软/苹果/游戏/学术/广告拦截/GitHub/Spotify/兜底),地区分组按常见机场命名自动归类(港/台/日/新/韩/美/其他),不中可改各组 `filter` 正则。

## 目录布局

```
/opt/panixy/
├── bin/mihomo          # 内核(自动升级,保留3份备份)
├── ui/official/        # metacubexd 面板(自动升级)
│   └── .official.version  (上游 release tag 戳)
├── cache.db            # mihomo 家目录(-d):fake-ip 缓存/订阅/规则
├── proxies/SUB.yaml    # 订阅缓存(set-sub 预取预置,内核重启秒级加载)
├── rule_provider/      # 规则缓存(离线包预置)
├── GeoIP.dat GeoSite.dat Country.mmdb
├── .last-upgrade       # 最近一次升级成功时间戳
/etc/clash.yaml         # 配置
/usr/local/bin/panixy   # 本工具
```

## 要求

- systemd Linux,x86_64(需支持 AVX2)或 aarch64,bash + curl
- root 权限安装

## 手工配置(自己调好的 clash.yaml)

三种放法,优先级从高到低:
1. 装之前放到 `/etc/clash.yaml` —— 安装器检测到就原样采用
2. 装之前放到**包根目录** `Panixy-V0.0.1/clash.yaml` —— 安装器优先于模板采用
   (⚠️ 该文件含真实订阅链接,已被 .gitignore 排除,不会误提交)
3. 装好之后随时替换:`panixy check 你的.yaml` 校验 → `sudo panixy apply-conf 你的.yaml` 生效
   (apply-conf/set-sub 优先**热重载**:不重启、不重建 TUN;失败自动退回重启,再失败恢复原配置)

## 命令

| 命令 | 作用 |
|---|---|
| `sudo panixy install` | 部署 unit/timer、开 ip_forward、拉起服务(**失败自动回滚**) |
| `sudo panixy set-sub <URL> [本地文件]` | 导入订阅:预取(直连→本机代理两级,或用本地文件)→预置缓存→重启→**验证节点加载**,失败自动恢复原订阅 |
| `panixy check [yaml]` | 用内核 `-t` 校验配置文件(只读,免 root) |
| `sudo panixy apply-conf <yaml>` | 部署手工调整的配置(优先热重载;失败自动恢复原配置) |
| `sudo panixy upgrade` | 内核+UI 升级(timer 每天 04:17±25min 自动跑) |
| `sudo panixy update-ui` | 单独触发面板升级 |
| `panixy status` | 版本/服务/DNS/代理健康一览(含上次升级成功时间) |
| `sudo panixy rollback [v1.19.x]` | 回滚内核(默认最近备份) |
| `sudo panixy uninstall` | 移除 unit/timer/sysctl(数据保留) |
| `panixy log [行数]` | 查看服务与自动升级日志 |

## 自动升级机制(参考 TPClash 的职责划分,但为真·运行时更新)

TPClash 把 UI 编进二进制、内核只在缺失时下载——都不会更新。panixy 的做法:

- **内核**:查 `MetaCubeX/mihomo` 最新稳定版 → 经本机代理下载(直连兜底)→ 试运行校验版本
  → 原子替换 → 重启 → 双健康检查(API 版本 + 经代理出网 204)→ 失败自动回滚
- **UI**:查 `MetaCubeX/metacubexd` 最新 release → 比对 `.official.version` 戳 →
  下载 `compressed-dist.tgz` → 换目录 → `GET /ui/` 探活 → 失败恢复旧版
- x86 升级时 V3 优先,下载后试运行验证,指令集不兼容自动降级标准版/compatible 版
- 全部成功会更新 `/opt/panixy/.last-upgrade` 时间戳,`panixy status` 展示——
  时间过旧说明升级在静默停滞(API 限流/网络),查 `panixy log`

## 订阅导入与无外网初始化(为什么是"预取+重启+验证")

实测 mihomo 行为决定了这套流程:

- **拉不到订阅时内核照常运行、API 照常应答**(组里只有空的 COMPATIBLE)——只查 API 的健康检查会"假成功"
- **热重载不重建 provider**:换了订阅 URL 热重载后,内核仍按旧 URL 拉取,新订阅永远不生效
- **有本地缓存时内核秒级加载、不联网**;缓存陈旧则先加载旧内容再后台刷新(刷新流量经自身规则走节点)

因此 `set-sub` 的做法:CLI 用与内核一致的 UA(`clash.meta/vX.Y.Z`)预取订阅(机场按 UA 返回 Clash 格式)→
校验内容确为含节点的 YAML → 写入配置并**预置缓存**(`proxies/SUB.yaml`)→ 重启重建 provider →
轮询 API 直到**节点数 > 0** 才算成功;任一步失败恢复原配置与缓存。拉取顺序:本地文件 > 直连 > 经本机代理
(换被墙订阅时旧节点还能当跳板);都不通时报可执行的离线导入指引。

广告分流规则源在 `raw.githubusercontent.com`(国内直连不可达),离线包已预置规则缓存,首启即生效;
内核后台刷新规则/订阅的流量会经自身规则走节点,节点通了自然恢复更新。

## 开发与测试

改完 `panixy`/模板后跑冒烟测试(沙箱内模拟假 systemd、假订阅服务器与不可达网络,**不动系统、不需要 root、不联外网**):

```bash
tests/smoke.sh            # 内核默认取 /opt/panixy/bin/mihomo,可用 tests/smoke.sh <内核路径> 覆盖
```

覆盖:模板过内核 `-t`、可达订阅导入并验证节点数、不可达订阅诚实失败且配置零改动、被墙订阅+本地文件离线导入、旧配置自动补 `path`、`status` 节点行。

## 架构

```
LAN 客户端 ──(网关=本机,ip_forward)──> TUN "Meta"
本机应用 ── resolved(~. → 198.18.0.2)──> mihomo DNS(fake-ip)
mihomo ── auto-route/auto-detect-interface ──> 物理网卡出站
```

- `panixy.service`:ExecStartPre 配置校验(-t,坏配置拒启);ExecStartPost 等 TUN 就绪后
  `resolvectl dns Meta 198.18.0.2 + domain ~.`;停止时 revert,系统 DNS 自动降级直连
- 配置即事实源:`secret`、`mixed-port`、`external-controller` 改了 `/etc/clash.yaml`,
  CLI 下次运行自动跟随,**无需再同步工具侧**(可用环境变量 PANIXY_SECRET/PROXY/API 覆盖)

## 注意

1. API 监听 0.0.0.0。密钥在安装时**随机生成并打印**(模板路径;自带配置沿用你自己的),
   查看:`grep '^secret' /etc/clash.yaml`。网关多网口时仍建议用防火墙限制 9999 来源
2. LAN 客户端 DNS:本方案靠 TUN `any:53` 劫持——DHCP 给客户端下发**公网 DNS**(如
   223.5.5.5)即可被劫持;若 DHCP 下发网关 IP 本身,53 端口无人监听,需加 dnsmasq
   转发到 1053,或改 DHCP 下发公网地址
3. 双病灶背景(配置模板已内置修复):`stack: gvisor` 治链路事件后栈卡死;
   `prefer-h3: false` 治 UDP 间歇故障时 DNS 上游陪葬。节点尽量保留 TCP 协议(Vless/Trojan)兜底

## 故障排查

- `status` 显示"节点: 0 ⚠️ 订阅未加载":订阅没导入成功——重新 `set-sub`(可带本地订阅文件离线导入),仍失败看 `panixy log` 中内核报错
- 全机断网:`systemctl restart panixy`,恢复后查 `panixy log`
- 只断代理(直连正常):多为运营商 UDP 波动,通常 1-2 分钟自愈;持续则换 TCP 节点
- DNS 异常:`resolvectl dns Meta` 应显示 198.18.0.2;没有则 `systemctl restart panixy`
- set-sub/apply-conf 热重载后怀疑没生效:极少数字段需重启,`sudo systemctl restart panixy` 一次即可
