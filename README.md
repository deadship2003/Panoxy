<div align="center">

# Panixy

**基于 [mihomo](https://github.com/MetaCubeX/mihomo) 内核的 Linux 透明代理网关部署/管理工具**

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20amd64%7Carm64-lightgrey)]()
[![Release](https://img.shields.io/badge/Release-V0.1.0-orange)](../../releases)

单二进制 · 零依赖 · 事务式部署 · 全量回滚

</div>

---

## ✨ 特性

- 🔧 **TUN / TPROXY 双模式** — TUN 开箱稳定(默认);TPROXY 保留客户端真实源 IP、内核转发性能最优
- 🛡️ **DNS 劫持 = nftables** — 独立表 `inet panixy`,53 redirect → mihomo:1053,拒绝 853(DoT/DoQ)
- 🔄 **自愈** — kill -9/OOM 残留随 `systemctl restart panixy` 自动清除,无需手工干预
- 📡 **订阅可验证** — 预取 → 校验 → 增量写入 → 重启 → **节点数 > 0 才算成功**,绝不假成功
- 🧩 **配置融合** — `merge-conf` 同名组字段级合并(proxies/use 并集),基底组保留不删
- ⬆️ **参数化升级** — `--core/--ui/--check/--core-version`,试运行校验、失败自动回滚
- 📖 **全量文档** — `-h/-?/--help` 每命令含示例;`man panixy` 与 `--help` 同源生成
- 🔍 **调试友好** — `--verbose` 分步明细;`--debug` 外部命令/API I/O 零遮蔽

## 🚀 快速开始

### 方式一:单二进制直装(自用,推荐)

```bash
# 拷贝 panixy 二进制到目标机器,然后:
sudo panixy init '你的订阅链接'
```

九步自动完成:预检 → 取订阅 → 网络探测 → 下载内核 → 下载 geo/规则 → 下载面板 → 资产就位 → 部署服务 → 导入订阅。
每步带进度条,断网环境自动经订阅节点建立代理下载。

### 方式二:离线包(给朋友)

从 [Releases](../../releases) 下载离线包(34MB,含内核+geo+UI+规则):

```bash
tar xzf Panixy-V0.1.0-amd64.tar.gz && cd Panixy-V0.1.0-amd64
sudo ./panixy deploy                 # 全自动安装
sudo panixy set-sub                  # 粘贴订阅链接(免引号)
panixy status                        # 验证健康
```

### 方式三:预安装(免 root 试跑)

```bash
panixy try '订阅链接'                 # 沙箱实测完整安装,不触碰真实系统
panixy init --dry-run                # 只读预演(环境/下载策略/配置渲染)
```

### 已有个人配置?

```bash
sudo panixy merge-conf ~/我的.yaml    # 叠加融合:同名组合并,基底组保留
sudo panixy merge-conf --dry-run ~/我的.yaml   # 先看融合决策
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

## 📂 仓库布局

```
Panixy/
├── src/               Go 源码(cmd/internal/tests)
├── dist/              发布产物(二进制+离线包,gitignored)
├── scripts/           编译/打包脚本
├── docs/              扩展文档
│   ├── TPROXY.md      TPROXY 模式完整指南
│   ├── MIGRATION.md   bash 版迁移步骤
│   ├── KNOWN-LIMITATIONS.md
│   └── TROUBLESHOOTING.md
├── legacy/            旧 bash 版归档
├── Makefile           一键入口
└── README.md
```

## 🛠️ 编译

### 前提

- Go 1.23+([安装](https://go.dev/dl/))
- 无需 CGO 依赖(纯静态编译)

### 用 Makefile(推荐)

```bash
make build                    # 编译双架构 → dist/
make build VERSION=V0.1.0    # 指定版本号
```

### 用脚本

```bash
./scripts/build.sh V0.1.0     # 与 make build 等效
./scripts/build.sh -h         # 查看帮助
```

### 手工编译

```bash
cd src

# 本机架构(amd64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.1.0" \
  -o ../dist/panixy-linux-amd64 \
  ./cmd/panixy

# 交叉编译 ARM64(无需 ARM 机器)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.1.0" \
  -o ../dist/panixy-linux-arm64 \
  ./cmd/panixy

# 生成校验和
cd ../dist && sha256sum panixy-linux-* > sha256sums.txt
```

<details>
<summary>📖 编译参数说明</summary>

| 参数 | 作用 |
|---|---|
| `CGO_ENABLED=0` | 纯静态编译,无 libc 依赖,任意 Linux 可跑 |
| `-trimpath` | 去掉编译机路径信息(安全+体积) |
| `-ldflags "-s -w"` | 去掉符号表和调试信息(体积减 30%) |
| `-X main.version=X` | 注入版本号(`panixy --version` 显示) |
| `GOAMD64=v1` | 兼容所有 x86_64 CPU(不用 v3 指令集) |

</details>

### 验证编译产物

```bash
file dist/panixy-linux-amd64
# ELF 64-bit LSB executable, x86-64, statically linked ✓

dist/panixy-linux-amd64 --version
# panixy version V0.1.0
```

## 📦 打包

### 用 Makefile(推荐)

```bash
make package VERSION=V0.1.0         # 打当前架构离线包 → dist/
make package-all VERSION=V0.1.0     # 双架构 → dist/
```

### 用脚本

```bash
./scripts/package.sh --ver V0.1.0                     # 当前架构
./scripts/package.sh --arch all --ver V0.1.0         # 双架构
./scripts/package.sh -h                               # 查看帮助
```

### 脚本支持的参数/环境变量

| 参数/变量 | 默认 | 说明 |
|---|---|---|
| `--arch amd64\|arm64\|all` | 当前平台 | 目标架构 |
| `--ver V0.1.0` | git describe | 版本号 |
| `--sub-url URL` | (空) | 断网时经订阅代理下载资产 |
| `ASSETS_SRC` | `/opt/panixy` | 本地资产目录(存在则复制,不下载) |
| `MIHOMO_VERSION` | `v1.19.30` | 内核版本 |
| `PROXY_PORT` | `33999` | 订阅引导代理端口 |

### 打包流程(内部步骤)

```
[1/5] 编译 ─── 调用 build.sh → dist/panixy-linux-{amd64,arm64}
[2/5] 资产 ─── 本地优先(ASSETS_SRC)> 直连(15s 检测)> 订阅代理 > gh 镜像
                下载: mihomo 内核 + geo×3 + Country.mmdb + AWAvenue 规则 + metacubexd UI
[3/5] 扫描 ─── 订阅泄露检测(token= 等特征命中即中止,URL 永不进包)
[4/5] 组装 ─── Panixy-V<ver>-<arch>/{panixy, README.md, assets/}
[5/5] 打包 ─── tar.gz + sha256 → dist/
```

### 手工打包

<details>
<summary>📖 展开手工打包完整步骤</summary>

```bash
cd ~/Panixy
mkdir -p dist

# ===== 第 1 步:编译 =====
cd src
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.1.0" \
  -o ../dist/panixy-linux-amd64 ./cmd/panixy
cd ..

# ===== 第 2 步:下载资产 =====
TMP=$(mktemp -d)
MIHOMO_VER="v1.19.30"

# mihomo 内核(18MB)
curl -fsSL -o $TMP/mihomo.gz \
  "https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER/mihomo-linux-amd64-v3-$MIHOMO_VER.gz"

# geo 三件(28MB)
geo="https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
curl -fsSL -o $TMP/GeoIP.dat    "$geo/geoip.dat"
curl -fsSL -o $TMP/GeoSite.dat  "$geo/geosite.dat"
curl -fsSL -o $TMP/Country.mmdb "$geo/country.mmdb"

# 广告规则
curl -fsSL -o $TMP/AWAvenue-Ads.yaml \
  "https://raw.githubusercontent.com/TG-Twilight/AWAvenue-Ads-Rule/refs/heads/main/Filters/AWAvenue-Ads-Rule-Clash-Classical.yaml"

# metacubexd 面板
curl -fsSL -o $TMP/ui.tgz \
  "https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz"

# ===== 第 3 步:组装离线包 =====
PKG="Panixy-V0.1.0-amd64"
rm -rf "$PKG"
mkdir -p "$PKG/assets/core" "$PKG/assets/geo" "$PKG/assets/ui/official" "$PKG/assets/rule"

cp dist/panixy-linux-amd64 "$PKG/panixy"
chmod +x "$PKG/panixy"
cp $TMP/mihomo.gz "$PKG/assets/core/"
cp $TMP/Geo*.dat $TMP/Country.mmdb "$PKG/assets/geo/"
cp $TMP/AWAvenue-Ads.yaml "$PKG/assets/rule/"
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
gzip -c /opt/panixy/bin/mihomo > "$PKG/assets/core/mihomo-linux-amd64-v1.19.30.gz"
cp /opt/panixy/Geo*.dat /opt/panixy/Country.mmdb "$PKG/assets/geo/"
cp /opt/panixy/rule_provider/AWAvenue-Ads.yaml "$PKG/assets/rule/"
```

</details>

### 最终包内结构

```
Panixy-V0.1.0-amd64/
├── panixy                                    ← Go 二进制(9MB)
├── README.md
└── assets/
    ├── core/mihomo-linux-amd64-v1.19.30.gz  ← 内核(18MB)
    ├── geo/GeoIP.dat GeoSite.dat Country.mmdb
    ├── rule/AWAvenue-Ads.yaml                ← 广告规则
    └── ui/official/                          ← metacubexd 面板(161 文件)
```

**总计约 34MB** · 用户拿到后 `tar xzf` → `sudo ./panixy deploy` 即完成安装。

### CI 自动打包

推 `V*` 标签触发 GitHub Actions,与本地 `scripts/package.sh` 同一脚本:

```bash
git tag V0.1.0 && git push origin V0.1.0
# → CI 自动编译+打包+发布 Release
```

**订阅 URL 永不进包**:打包前自动扫描 `token=` 等特征,命中即中止。

## 📋 命令参考

| 命令 | 作用 |
|---|---|
| `panixy try [URL]` | 预安装(免 root 沙箱实测) |
| `panixy init/deploy --dry-run` | 试运行模式(免 root) |
| `sudo panixy init [URL]` | 裸机初始化(九步带进度) |
| `sudo panixy deploy [URL]` | 从离线包部署 |
| `sudo panixy merge-conf <yaml>` | 个人配置叠加融合(`--dry-run`/`--rollback`) |
| `sudo panixy set-sub [URL]` | 导入订阅(粘贴模式免引号) |
| `sudo panixy sub-rm --name N` | 删除订阅 |
| `panixy sub-list [--json]` | 各订阅状态/节点数 |
| `panixy status [-v\|-q\|--json]` | 健康一览(`-q` 退出码供监控) |
| `sudo panixy mode [tun\|tproxy]` | 查看/切换模式 |
| `sudo panixy upgrade [--core\|--ui] [--check]` | 参数化升级 |
| `sudo panixy rollback [vX]` | 内核回滚 |
| `panixy check [yaml]` | 校验配置 |
| `sudo panixy apply-conf <yaml>` | 应用配置(热重载优先) |
| `sudo panixy uninstall` | 卸载(保留数据) |
| `panixy man [命令]` | 查看手册(根页或子命令页) |
| `sudo panixy fw <apply\|teardown\|clean>` | 防火墙管理 |

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

| 层级 | 测什么 | panixy 中 | 数量 | 速度 |
|---|---|---|---|---|
| 单元 | 单个函数 | YAML 融合/防火墙规则生成/模板渲染 | ~15 | <1s |
| 集成 | 组件配合 | 配置过 mihomo `-t` | ~5 | 1-2s |
| E2E | 完整流程 | deploy→set-sub→status 全链路 | 3 | ~50s |

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
| `panixy man` | 手册(部署后 `man panixy` / `man panixy-<命令>`) |

## 📄 License

MIT

---

<div align="center">
<sub>Built with Go · Powered by <a href="https://github.com/MetaCubeX/mihomo">mihomo</a></sub>
</div>
