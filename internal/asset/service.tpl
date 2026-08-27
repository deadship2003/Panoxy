[Unit]
Description=panixy mihomo transparent proxy ({{.Mode}})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
# 启动前配置校验(mihomo -t;其日志走 stdout,由 journald 收)
ExecStartPre={{.Bin}} -t -f {{.Conf}} -d {{.Root}}
ExecStart={{.Bin}} -f {{.Conf}} -d {{.Root}} -ext-ui {{.UiDir}}
# DNS 劫持规则:apply 内部先无条件 CleanAll 再加载 —— kill -9/OOM 残留随 restart 自愈
ExecStartPost={{.Cli}} fw apply
ExecStop={{.Cli}} fw teardown
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
