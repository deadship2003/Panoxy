# TPROXY 模式指南

### TPROXY 模式详细指南

**前置检测(物理机/真机)**:
```bash
grep -w TPROXY /proc/net/ip_tables_targets    # 有输出 = 可用
sudo modprobe xt_TPROXY                       # 无输出时尝试加载
```
Arch/Debian/Ubuntu 标准内核默认包含;WSL2 微信裁剪内核不支持。

**切换**:
```bash
sudo panixy mode tproxy     # 原子切换:旧规则卸载→配置变体→重启→新规则→健康检查
sudo panixy mode tun        # 切回 TUN(自动清理 TPROXY 规则)
```

**切换后验证**:
```bash
panixy mode                              # "tproxy"
ip rule show | grep fwmark              # fwmark 0x1 lookup 100
ip route show table 100                 # local default dev lo
sudo nft list table inet panixy | grep tproxy
```

**TUN vs TPROXY 对比**:

| 对比项 | TUN(默认) | TPROXY |
|---|---|---|
| 源 IP | ❌ 丢失(显示为网关 IP) | ✅ **保留客户端真实 IP** |
| 性能 | gvisor 用户态 | 内核转发,**理论最优** |
| 配置复杂度 | 低(auto-route) | 中(mark/策略路由) |
| 内核要求 | TUN 驱动 | xt_TPROXY 模块 |
| Docker/容器 | 兼容好 | 可能误劫持 |
| WSL2/虚拟化 | ✅ | ❌(内核裁剪) |

**透明网关网络拓扑**:
```
Internet ←─ WAN ── panixy 机器(LAN 口 192.168.1.1)── LAN 设备
                    │                        ↑
                    │ nftables DNS 劫持       │ DHCP 网关=192.168.1.1
                    │ TPROXY mark+tproxy      │ DNS=公网(53 被劫持)
                    └─ mihomo :7893          │
                                              └─ 设备无需任何配置
```

**LAN 设备接入(三选一)**:
1. 路由器 DHCP 下发网关 = panixy 机器 LAN IP,DNS = 公网地址
2. 单台设备手动设网关指向 panixy 机器
3. panixy 机器自身跑 DHCP(dnsmasq 示例):
```bash
sudo tee /etc/dnsmasq.conf << 'EOF'
interface=eth0
dhcp-range=192.168.1.100,192.168.1.200,12h
dhcp-option=3,192.168.1.1
dhcp-option=6,223.5.5.5
EOF
sudo systemctl enable --now dnsmasq
```

**故障排查**:
- 切换后断网:`sudo systemctl restart panixy`(自愈)
- 策略路由丢:`ip rule show | grep fwmark` 确认;`sudo panixy fw apply` 重载
- 某设备不走代理:检查其网关是否指向 panixy 机器
