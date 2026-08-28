[Unit]
Description=panixy core & UI auto-upgrade (oneshot)
After=network-online.target

[Service]
Type=oneshot
Environment=PANIXY_ROOT={{.Root}}
ExecStart={{.Cli}} upgrade
