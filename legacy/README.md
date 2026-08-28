# legacy —— bash 版遗留(已归档,不参与构建)

Go 重构前的 bash 版实现,仅留档对照;**勿部署使用**(无防火墙 DNS 劫持方案,
单元含 resolvectl 接管,与 Go 版不兼容)。

- `panixy.bash` V0.0.2:bash 单文件版 CLI(set-sub 粘贴模式/自定义 provider 名等
  经验均已带入 Go 版)
- `clash-template.yaml` v0.1.4:bash 版配置模板(Go 版模板内嵌于
  `internal/asset/config.tpl`,含防火墙暗号 routing-mark/listen 1053)
- `smoke.sh`:bash 版冒烟测试(Go 版测试在 `tests/` 与 `internal/*/_test.go`)

现行版本:Go(入口 `cmd/panixy`,构建 `scripts/build.sh`)。
