## 已知限制(必读)

1. **热重载不刷新 proxy-providers**(内核限制):sub import/del/mode 一律重启进程生效
2. kill -9/OOM 会残留防火墙规则:`systemctl restart Panoxy` 启动即自动清理,无需手工
3. **DoH(443)无法在内核劫持**:浏览器内置加密 DNS 不走分流,status 已提示,建议关闭
4. 订阅预取只是预校验;运行期内核会按 interval 自行远程拉取
5. sub import `--name` 依赖配置锚点 `&p`(基础模板自带;纯手写配置需自备)
6. tun `stack: system` 家用默认;重度 BT/长时 UDP 流媒体/节点频繁掉线/老内核(5.4/5.15)建议改 `gvisor`(进程崩溃可被 systemd 自动拉起,优于静默僵死)
7. **二进制按「编译机」CPU 选型**:内核已内嵌于 CLI,`build.sh` 在编译时探测本机 AVX2 决定 GOAMD64(有→v3,无→v1);跨 CPU 类别部署(有 AVX2 的机器编译 → 无 AVX2 的老机器)会跑不起来。要全兼容用 `GOAMD64=v1 ./build.sh`(或用无 AVX2 的机器编译)。
