# Panoxy TPROXY 模式：网关本机断网修复方案

> 适用：Panoxy（Go 版；配置 v0.2.0 / CLI v0.0.1）。用于多主机、多分支同步排查。

## 1. 现象

- **TUN 模式正常**；`sudo Panoxy mode tproxy` 切过去后，**网关本机**上的应用连不上外网（典型表现：跑在网关上的 Claude Code 一直连不上 `api.anthropic.com`）。
- **LAN 客户端走代理正常**（它们走 PREROUTING，另一条路径），所以容易被误判成「只有 Claude 断」。

## 2. 根因

**部署的 CLI 二进制 `/usr/local/bin/Panoxy` 是旧的，没编译进 TPROXY 修复提交。**

修复提交 `6664d5c`（`feat(firewall): align TPROXY traffic model fully equivalent to TUN`）改的是
`src/internal/firewall/rules.go` 的 `BuildNftTproxyScript`：

- **新增 `local_output` 链**：`type route hook output priority mangle`，把本机出站 tcp/udp 打 `mark 0x1`
  → 经策略路由 `ip rule fwmark 1 lookup 100` + `ip route add local 0.0.0.0/0 dev lo table 100`
  → 回环重入 → 再走 `tproxy_prerouting` 交给 mihomo（与 TUN 等价）。
- **删除 `tproxy_prerouting` 里的 `iifname "lo" return`**：该行会把回环重入的本机流量直接放行，导致本机流量永远到不了 `tproxy to :7893`。

旧规则下，本机出站流量既不被打标、又被 `iifname "lo" return` 放行直连 → 域名解析出的
fake-ip（`198.18.0.x`）被按默认路由甩给网关后被黑洞 → **本机整体断网**（国内外全断，实测 baidu 也超时）。

## 3. 判定（分支无关，3 条命令）

在任何一台主机上先判断「是否中招」「源码有没有修复」：

```bash
# ① 部署的二进制是否含修复（决定性判定）
#    输出 1 = 已修复；0 = 旧二进制（中招）
strings -n 6 /usr/local/bin/Panoxy | grep -c 'chain local_output'

# ② 源码是否含修复
#    输出 1 = 源码已有修复；0 = 源码也旧
grep -c 'chain local_output' /path/to/Panoxy/src/internal/firewall/rules.go

# ③ 二进制编译自哪个 commit vs 源码 HEAD（看分支落后多少）
strings /usr/local/bin/Panoxy | grep -oE 'V[0-9.]+-[0-9]+-g[0-9a-f]+' | head -1   # 如 V0.0.1-5-g84117d8
git -C /path/to/Panoxy/src rev-parse --short HEAD                                  # 源码当前 HEAD
```

- ①=0 → 本机必中招，按第 4 节修复。
- ①=0 且 ②=0 → 源码分支落后于 `6664d5c`，先合并/挑选该提交（情况 A）。
- ①=0 且 ②=1 → 源码已有修复、只是二进制没重编（情况 B，最常见）。

## 4. 修复

### 情况 A：源码分支没有修复提交

把 `6664d5c` 合入当前分支（或 cherry-pick）：

```bash
cd /path/to/Panoxy/src
git fetch origin
git cherry-pick 6664d5c        # 或 git merge 含该提交的远端分支
```

### 情况 B：源码已有修复，重编 + 重部署

```bash
cd /path/to/Panoxy

# 1) 备份旧二进制
sudo cp /usr/local/bin/Panoxy /usr/local/bin/Panoxy.bak-$(strings /usr/local/bin/Panoxy | grep -oE 'g[0-9a-f]{7}' | head -1)

# 2) 重编（版本号由 git describe 自动注入）
make build

# 3) 安装
sudo make install      # 等价 sudo install -Dm755 dist/Panoxy-linux-amd64 /usr/local/bin/Panoxy
```

> 若某主机没有完整仓库/没有 go 工具链，也可直接拿一台已编译好的 `dist/Panoxy-linux-amd64`
> 拷过去覆盖 `/usr/local/bin/Panoxy`（二进制是纯静态的，`CGO_ENABLED=0`）。

## 5. 验证

```bash
# 安装后再确认二进制已更新（应为 1，且版本 g6664d5c 或更新）
strings -n 6 /usr/local/bin/Panoxy | grep -c 'chain local_output'

# 切 TPROXY 实测本机出站
sudo Panoxy mode tproxy
curl -sS -o /dev/null -w "HTTP=%{http_code} ip=%{remote_ip}\n" --max-time 10 https://api.anthropic.com/v1/messages
#   → HTTP=405 即走代理通了（GET 到 /v1/messages 无凭据正常返回 405）
#   → 再测国内直连：curl -sS -o /dev/null -w "%{http_code}\n" --max-time 10 https://www.baidu.com  → 200

# 保持 tproxy 或切回
sudo Panoxy mode tun
```

## 6. 关键参照

- **修复提交**：`6664d5c` `feat(firewall): align TPROXY traffic model fully equivalent to TUN`
  （完整 `6664d5cf42cb02a04f08a219a09f7d74ce7484cd`）
- **关键代码**：`src/internal/firewall/rules.go` → `BuildNftTproxyScript`（`local_output` 链 + `tproxy_prerouting`）
- **策略路由**：`src/internal/firewall/policy.go` → `tproxyPolicyCmds`
- **常量**：`src/internal/constants/constants.go`（`MarkTproxy=1`、`TproxyTable=100`、`TproxyPort=7893`、`MarkSelf=6666`）
- **配置渲染**：`src/internal/asset/config.tpl`（`.TProxy` 为真时输出 `tproxy-port: 7893` 并省略 `tun` 块）
