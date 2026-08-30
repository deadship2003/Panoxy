[Unit]
Description={{.Prog}} core & UI auto-upgrade (oneshot)
After=network-online.target

[Service]
Type=oneshot
Environment={{.EnvPrefix}}_ROOT={{.Root}}
ExecStart={{.Cli}} upgrade
