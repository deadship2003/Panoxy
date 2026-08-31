[Unit]
Description={{.Prog}} mihomo transparent proxy ({{.Mode}})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
# 自定义安装目录时,fw apply/upgrade 子进程据此找到状态与数据(与 --root 一致)
Environment={{.EnvPrefix}}_ROOT={{.Root}}
# 启动前配置校验(进程内 -t)
ExecStartPre={{.Cli}} check
# 进程内跑内核(融合 mihomo Go 代码;不再启动外部二进制)
ExecStart={{.Cli}} run
# DNS 劫持规则:apply 内部先无条件 CleanAll 再加载 —— kill -9/OOM 残留随 restart 自愈
ExecStartPost={{.Cli}} fw apply
ExecStop={{.Cli}} fw clean
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
