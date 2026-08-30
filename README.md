<div align="center">

# Panoxy

**基于 [mihomo](https://github.com/MetaCubeX/mihomo) 内核的 Linux 透明代理网关部署/管理工具**

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20amd64%7Carm64-lightgrey)]()
[![Release](https://img.shields.io/badge/Release-V0.0.1-orange)](../../releases)

单二进制 · 零依赖 · 事务式部署 · 全量回滚

</div>

> **程序名**：默认程序名为 `Panoxy`，可在编译期定制为任意名字（如 `myproxy`）。定制后，二进制名、安装路径、配置路径、systemd 单元、nft 表、iptables 链、环境变量前缀、man 手册**全部跟随**该名字。见[「编译 · 定制程序名」](#定制程序名)。

---

## ✨ 特性

- 🔧 **TUN / TPROXY 双模式** — TUN 开箱稳定(默认);TPROXY 保留客户端真实源 IP、内核转发性能最优
- 🛡️ **DNS 劫持 = nftables** — 独立表 `inet Panoxy`,53 redirect → mihomo:1053,拒绝 853(DoT/DoQ)
- 🔄 **自愈** — kill -9/OOM 残留随 `systemctl restart Panoxy` 自动清除,无需手工干预
- 📡 **订阅可验证** — 预取 → 校验 → 增量写入 → 重启 → **节点数 > 0 才算成功**,绝不假成功
- 🧩 **配置融合** — `merge-conf` 同名组字段级合并(proxies/use 并集),基底组保留不删
- ⬆️ **参数化升级** — `--core/--ui/--cli/--check/--core-version`,试运行校验、失败自动回滚
- 📖 **全量文档** — `-h/-?/--help` 每命令含示例;`man Panoxy` 与 `--help` 同源生成
- 🔍 **调试友好** — `--verbose` 分步明细;`--debug` 外部命令/API I/O 零遮蔽

## 🚀 快速开始

### 方式一:单二进制直装(自用,推荐)

```bash
# 拷贝 Panoxy 二进制到目标机器,然后:
sudo Panoxy init '你的订阅链接'
```

九步自动完成:预检 → 取订阅 → 网络探测 → 下载内核 → 下载 geo/规则 → 下载面板 → 资产就位 → 部署服务 → 导入订阅。
每步带进度条;直连不通时经订阅节点建立代理下载(需本机已有 mihomo 内核);无内核时提示用离线包 `deploy` 或手工复制内核到 `/opt/Panoxy/bin/mihomo`。

### 方式二:离线包(给朋友)

从 [Releases](../../releases) 下载离线包(34MB,含内核+geo+UI+规则):

```bash
tar xzf Panoxy-V0.0.1-amd64.tar.gz && cd Panoxy-V0.0.1-amd64
sudo ./Panoxy deploy                 # 全自动安装
sudo Panoxy sub import              # 粘贴订阅链接(免引号)
Panoxy status                        # 验证健康
```

### 方式三:预安装(免 root 试跑)

```bash
Panoxy try '订阅链接'                 # 沙箱实测完整安装,不触碰真实系统
Panoxy init --dry-run                # 只读预演(环境/下载策略/配置渲染)
```

### 已有个人配置?

```bash
sudo Panoxy merge-conf ~/我的.yaml    # 叠加融合:同名组合并,基底组保留
sudo Panoxy merge-conf --dry-run ~/我的.yaml   # 先看融合决策
```

## 📐 架构

```
                 DNS(53/853)                        数据流量(非53)
┌──────────┐  nft redirect → :1053  ┌─┐  路由表 → TUN 设备 → mihomo
│ TUN 模式 │ ─────────────────────► │同│
├──────────┤                        │一│  nft mark 1 + 策略路由
│TPROXY模式│  nft redirect → :1053  │套│  + tproxy → :7893(保留源 IP)
└──────────┘ ─────────────────────► └─┘
```

- 数据面(节点/组选择)在 **Web 面板**;传输面(tun/tproxy)在 **CLI**
- mihomo 自身出站 `routing-mark: 6666` 放行 → 防 DNS 回环死锁
- systemd 单元零 resolvectl;`fw apply` 自清洁 → restart 自愈

### 流量策略(不阻断任何协议)

透明代理的第一目标是**正常访问**,分流只是优化走向:

| 协议 | 处理 | 说明 |
|---|---|---|
| 普通 DNS(53) | **劫持** → mihomo | 为大多数设备提供域名级分流(fake-ip) |
| QUIC/HTTP3(UDP 443) | **正常分流** | HTTP/3 原生体验;SNI 加密,域名规则不生效(IP 分流) |
| DoT(TCP 853) | **正常分流** | 加密 DNS 走代理(为自定义设备保留访问) |
| DoQ(UDP 853) | **正常分流** | 同上 |
| DoH(TCP 443) | **正常分流** | 与 HTTPS 同端口,无法也不应阻断 |

### 基础服务直连(32 条规则,不走代理)

| 类别 | 端口 | 服务 |
|---|---|---|
| **远程管理** | 22, 23 | SSH/SFTP, Telnet |
| **远程桌面** | 3389, 5900 | RDP, VNC |
| **VPN/组网** | 41641, 3478, 51820, 1194, 500, 4500, 1701, 1723 | Tailscale, STUN/TURN, WireGuard, OpenVPN, IPSec(IKE/NAT-T), L2TP, PPTP |
| **VoIP** | 5060, 5061 | SIP, SIPS |
| **域认证** | 88, 389, 636, 1812, 1813 | Kerberos, LDAP, LDAPS, RADIUS |
| **发现/时间** | 5353, 123, 161, 1900 | mDNS, NTP, SNMP, SSDP/UPnP |
| **IoT** | 1883, 8883, 5683 | MQTT, MQTT/TLS, CoAP |
| **存储/数据库** | 3260, 3306, 5432, 6379, 27017, 873 | iSCSI, MySQL, PostgreSQL, Redis, MongoDB, Rsync |
| **Tailscale 专属** | 100.100.100.100, 100.64.0.0/10 | MagicDNS, CGNAT 子网 |

## 📂 仓库布局

```
Panoxy/
├── src/               Go 源码(cmd/internal/tests)
├── dist/              发布产物(二进制+离线包,gitignored)
├── build.sh          打包分发脚本(离线包/订阅引导/泄露扫描)
├── docs/              扩展文档
│   ├── TPROXY.md      TPROXY 模式完整指南
│   ├── MIGRATION.md   bash 版迁移步骤
│   ├── KNOWN-LIMITATIONS.md
│   └── TROUBLESHOOTING.md
├── legacy/            旧 bash 版归档
├── Makefile           本机编译/安装入口(make)
└── README.md
```

## 🛠️ 编译

### 前提

- Go 1.23+([安装](https://go.dev/dl/))
- 无需 CGO 依赖(纯静态编译)

### 用 Makefile(推荐)

```bash
make                                    # 编译当前架构 → dist/(amd64 自动检测 AVX2)
make build                              # 同上(显式)
make install                            # 安装 CLI → /usr/local/bin/Panoxy(PREFIX/BINDIR 可自定义)
make build PANOXY_VERSION=V0.0.1        # 指定版本号
make build PROG=myproxy                 # 定制程序名(默认 Panoxy,见「定制程序名」)
```

### 用脚本

```bash
./build.sh                              # 编译当前架构(默认)
./build.sh --arch arm64                 # 指定架构
./build.sh --arch all                   # 双架构
./build.sh --ver V0.0.1                 # 指定版本
```

### 手工编译

```bash
cd src

# 本机架构(amd64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v3 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.0.1" \
  -o ../dist/Panoxy-linux-amd64 \
  ./cmd/panixy

# 交叉编译 ARM64(无需 ARM 机器)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.0.1" \
  -o ../dist/Panoxy-linux-arm64 \
  ./cmd/panixy

# 生成校验和
cd ../dist && sha256sum Panoxy-linux-* > sha256sums.txt
```

<details>
<summary>📖 编译参数说明</summary>

| 参数 | 作用 |
|---|---|
| `CGO_ENABLED=0` | 纯静态编译,无 libc 依赖,任意 Linux 可跑 |
| `-trimpath` | 去掉编译机路径信息(安全+体积) |
| `-ldflags "-s -w"` | 去掉符号表和调试信息(体积减 30%) |
| `-X main.version=X` | 注入版本号(`Panoxy --version` 显示) |
| `-X github.com/deadship2003/Panoxy/internal/constants.ProgName=X` | 注入程序名(默认 `Panoxy`;详见[「定制程序名」](#定制程序名)) |
| `GOAMD64` | amd64 编译档:`build.sh` 默认自动检测 AVX2(有→v3,无→v1);手工可显式 `GOAMD64=v3`/`v1` |

</details>

### 定制程序名

默认程序名 `Panoxy` 在 `internal/constants.ProgName` 中定义;编译期用 `-X` 注入即可改名,二进制与运行期路径**全部继承**:

```bash
# 改名为 myproxy:二进制名、安装路径、配置路径、systemd 单元、nft 表、环境变量前缀、man 手册全部跟随
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath \
  -ldflags "-s -w -X main.version=V0.0.1 -X github.com/deadship2003/Panoxy/internal/constants.ProgName=myproxy" \
  -o ../dist/myproxy-linux-amd64 \
  ./cmd/panixy
```

更省事的方式是交给构建入口:

```bash
make PROG=myproxy                     # Makefile:PROG 变量
./build.sh --prog myproxy             # build.sh:--prog 参数
PROG=myproxy ./build.sh package       # 或环境变量 PROG(打包同理)
```

改名后运行期产物随之派生(以 `myproxy` 为例):

| 维度 | 默认 `Panoxy` | 改名 `myproxy` |
|---|---|---|
| 二进制 / 安装路径 | `Panoxy` → `/usr/local/bin/Panoxy` | `myproxy` → `/usr/local/bin/myproxy` |
| 配置 / 根目录 | `/etc/Panoxy.yaml` · `/opt/Panoxy` | `/etc/myproxy.yaml` · `/opt/myproxy` |
| systemd 单元 | `Panoxy.service` 等 | `myproxy.service` 等 |
| nft 表 / iptables 链 | `inet Panoxy` · `PANOXY_DNS` | `inet myproxy` · `MYPROXY_DNS` |
| 环境变量前缀 | `PANOXY_` | `MYPROXY_`(程序名转大写、`-`→`_`) |
| man 手册 | `Panoxy.1.gz` · `man Panoxy` | `myproxy.1.gz` · `man myproxy` |

> 注意:程序名(编译期变量)与 GitHub 仓库名(`deadship2003/Panoxy`)是两回事——改名只影响二进制与运行期产物,不影响仓库与升级源。

> **CLI 与内核的 CPU 选型**：Panoxy CLI 编译时默认按当前 CPU **自动检测** GOAMD64（amd64 有 AVX2 → `v3`，无 → `v1` 全兼容；可 `GOAMD64=v1 ./build.sh` 强制覆盖）。mihomo 内核则由 `Panoxy init` / `Panoxy upgrade` / `build.sh package` 在**运行时探测本机架构与 AVX2**，据此下载匹配的内核（有 AVX2 → `v3`，否则 → 标准档，再失败降 `compatible`）；内核下载/入包后即缓存，**不再重复探测**。

### 验证编译产物

```bash
file dist/Panoxy-linux-amd64
# ELF 64-bit LSB executable, x86-64, statically linked ✓

dist/Panoxy-linux-amd64 --version
# Panoxy version V0.0.1
```

## 📦 打包

### 用 build.sh

```bash
./build.sh package                       # 当前架构(默认)
./build.sh package all                    # 全部目标平台(amd64+arm64)
./build.sh package --arch arm64 --ver V0.0.1
./build.sh package -h                    # 查看帮助
```

### 脚本支持的参数/环境变量

| 参数/变量 | 默认 | 说明 |
|---|---|---|
| `--arch amd64\|arm64\|all` | 当前平台 | 目标架构 |
| `--ver V0.0.1` | git describe | 版本号 |
| `--prog Panoxy`(或 `PROG` 环境变量) | `Panoxy` | 程序名(编译期注入,决定二进制/包名与运行期路径) |
| `--sub-url URL` | (空) | 断网时经订阅代理下载资产 |
| `ASSETS_SRC` | `/opt/Panoxy` | 本地资产目录(存在则复制,不下载) |
| `MIHOMO_VERSION` | 运行时探测上游最新 | 内核版本(显式指定可固定/复现) |
| `PROXY_PORT` | `33999` | 订阅引导代理端口 |

### 打包流程(内部步骤)

```
[1/5] 编译 ─── 内联 go build → dist/Panoxy-linux-<arch>(all 则双架构)
[2/5] 资产 ─── 本地优先(ASSETS_SRC)> 直连(15s 检测)> 订阅代理 > gh 镜像
                下载: mihomo 内核 + geo×3 + Country.mmdb + HyperADRules 规则 + metacubexd UI
[3/5] 扫描 ─── 订阅泄露检测(token= 等特征命中即中止,URL 永不进包)
[4/5] 组装 ─── Panoxy-V<ver>-<arch>/{Panoxy, README.md, assets/}
[5/5] 打包 ─── tar.gz + sha256 → dist/
```

### 手工打包

<details>
<summary>📖 展开手工打包完整步骤</summary>

```bash
cd ~/Panoxy
mkdir -p dist

# ===== 第 1 步:编译 =====
cd src
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.0.1" \
  -o ../dist/Panoxy-linux-amd64 ./cmd/panixy
cd ..

# ===== 第 2 步:下载资产 =====
TMP=$(mktemp -d)
# 运行时探测上游最新内核版本(不写死);断网时用本机 /opt/Panoxy/bin/mihomo -v 兜底
MIHOMO_VER="$(curl -fsSL --connect-timeout 8 https://api.github.com/repos/MetaCubeX/mihomo/releases/latest \
  | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"

# mihomo 内核(18MB):探测本机 AVX2 决定 v3/标准档(与 build.sh package 同源)
if grep -qw avx2 /proc/cpuinfo; then
  curl -fsSL -o "$TMP/mihomo-linux-amd64-$MIHOMO_VER.gz" \
    "https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER/mihomo-linux-amd64-v3-$MIHOMO_VER.gz"
else
  curl -fsSL -o "$TMP/mihomo-linux-amd64-$MIHOMO_VER.gz" \
    "https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER/mihomo-linux-amd64-$MIHOMO_VER.gz"
fi

# geo 三件(28MB)
geo="https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
curl -fsSL -o $TMP/GeoIP.dat    "$geo/geoip.dat"
curl -fsSL -o $TMP/GeoSite.dat  "$geo/geosite.dat"
curl -fsSL -o $TMP/Country.mmdb "$geo/country.mmdb"

# 广告规则
curl -fsSL -o $TMP/HyperADRules-Ads.yaml \
  "https://github.com/Lynricsy/HyperADRules/releases/latest/download/hyper_adrules_ads_clash.yaml"

# metacubexd 面板
curl -fsSL -o $TMP/ui.tgz \
  "https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz"

# ===== 第 3 步:组装离线包 =====
PKG="Panoxy-V0.0.1-amd64"
rm -rf "$PKG"
mkdir -p "$PKG/assets/core" "$PKG/assets/geo" "$PKG/assets/ui/official" "$PKG/assets/rule"

cp dist/Panoxy-linux-amd64 "$PKG/Panoxy"
chmod +x "$PKG/Panoxy"
cp "$TMP/mihomo-linux-amd64-$MIHOMO_VER.gz" "$PKG/assets/core/"
cp $TMP/Geo*.dat $TMP/Country.mmdb "$PKG/assets/geo/"
cp $TMP/HyperADRules-Ads.yaml "$PKG/assets/rule/"
tar xzf $TMP/ui.tgz -C "$PKG/assets/ui/official"
cp README.md "$PKG/"

# ===== 第 4 步:打 tar 包 =====
tar -czf "dist/$PKG.tar.gz" "$PKG"
(cd dist && sha256sum "$PKG.tar.gz" > "$PKG.tar.gz.sha256")

# ===== 第 5 步:清理 =====
rm -rf "$PKG" $TMP
echo "产物: dist/$PKG.tar.gz ($(du -h dist/$PKG.tar.gz | cut -f1))"
```

**本机已有资产时跳过下载:**

```bash
# 内核版本取自本机已装内核(断网打包;与 build.sh package 的本地兜底同源)
MIHOMO_VER="$(/opt/Panoxy/bin/mihomo -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
gzip -c /opt/Panoxy/bin/mihomo > "$PKG/assets/core/mihomo-linux-amd64-$MIHOMO_VER.gz"
cp /opt/Panoxy/Geo*.dat /opt/Panoxy/Country.mmdb "$PKG/assets/geo/"
cp /opt/Panoxy/rule_provider/HyperADRules-Ads.yaml "$PKG/assets/rule/"
```

</details>

### 最终包内结构

```
Panoxy-V0.0.1-amd64/
├── Panoxy                                    ← Go 二进制(9MB)
├── README.md
└── assets/
    ├── core/mihomo-linux-amd64-<版本>.gz  ← 内核(18MB)
    ├── geo/GeoIP.dat GeoSite.dat Country.mmdb
    ├── rule/HyperADRules-Ads.yaml           ← 广告规则
    └── ui/official/                          ← metacubexd 面板(161 文件)
```

**总计约 34MB** · 用户拿到后 `tar xzf` → `sudo ./Panoxy deploy` 即完成安装。

### CI 自动打包

推 `V*` 标签触发 GitHub Actions,与本地 `build.sh package` 同一脚本:

```bash
git tag V0.0.1 && git push origin V0.0.1
# → CI 自动编译+打包+发布 Release
```

**订阅 URL 永不进包**:打包前自动扫描 `token=` 等特征,命中即中止。

## 📋 命令参考

| 命令 | 作用 |
|---|---|
| `Panoxy try [URL]` | 预安装(免 root 沙箱实测) |
| `Panoxy init/deploy --dry-run` | 试运行模式(免 root) |
| `sudo Panoxy init [URL]` | 裸机初始化(九步带进度) |
| `sudo Panoxy deploy [URL]` | 从离线包部署 |
| `sudo Panoxy redeploy` | 就地重装:强制刷新全部程序文件(保留配置),重挂防火墙并重启 |
| `sudo Panoxy merge-conf <yaml>` | 个人配置叠加融合(`--dry-run`/`--rollback`) |
| `Panoxy config [--mode tun\|tproxy] [--write]` | 打印默认配置模板(免 root;`--write` 写回 config.default.yaml) |
| `sudo Panoxy sub import [URL]` | 导入订阅(粘贴模式免引号) |
| `sudo Panoxy sub del --name N` | 删除订阅 |
| `Panoxy sub list [--json]` | 各订阅状态/节点数 |
| `Panoxy status [-q\|--json\|--detail]` | 健康一览(`-q` 退出码供监控) |
| `sudo Panoxy mode [tun\|tproxy]` | 查看/切换模式 |
| `sudo Panoxy upgrade [--core\|--ui\|--cli] [--check]` | 参数化升级 |
| `sudo Panoxy rollback [vX]` | 内核回滚 |
| `Panoxy check [yaml]` | 校验配置 |
| `sudo Panoxy apply-conf <yaml>` | 应用配置(热重载优先) |
| `sudo Panoxy uninstall` | 卸载(保留数据) |
| `Panoxy man [命令]` | 查看手册(根页或子命令页) |
| `sudo Panoxy fw <apply\|teardown\|clean>` | 防火墙管理 |

**全局参数**:`--root <dir>` 自定义安装目录 · `--verbose` 分步明细 · `--debug` 全量透蔽

## 🧪 测试

```bash
make test          # 单元测试(YAML 编辑器/防火墙规则文本/模板 -t)
make e2e           # 端到端测试(真实内核+假 systemd,约 60s)
make test-all      # 全部
make lint          # go vet
```

<details>
<summary>📖 测试金字塔说明</summary>

| 层级 | 测什么 | Panoxy 中 | 数量 | 速度 |
|---|---|---|---|---|
| 单元 | 单个函数 | YAML 融合/防火墙规则生成/模板渲染 | ~15 | <1s |
| 集成 | 组件配合 | 配置过 mihomo `-t` | ~5 | 1-2s |
| E2E | 完整流程 | deploy→sub import→status 全链路 | 3 | ~50s |

E2E 使用真实编译的二进制 + 真实 mihomo 内核 + 模拟订阅服务器 + 假 systemd,
不 mock 业务逻辑,验证用户实际体验。

</details>

## 📖 更多文档

| 文档 | 内容 |
|---|---|
| [docs/TPROXY.md](docs/TPROXY.md) | TPROXY 模式完整指南(前置检测/切换/验证/网络拓扑/故障排查) |
| [docs/MIGRATION.md](docs/MIGRATION.md) | 从 bash 版迁移步骤 |
| [docs/KNOWN-LIMITATIONS.md](docs/KNOWN-LIMITATIONS.md) | 已知限制(mihomo 限制/DoH/内核要求等) |
| [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | 故障排查指南 |
| `Panoxy man` | 手册(部署后 `man Panoxy` / `man Panoxy-<命令>`) |

## 📄 License

MIT

---

<div align="center">
<sub>Built with Go · Powered by <a href="https://github.com/MetaCubeX/mihomo">mihomo</a></sub>
</div>
