[Unit]
Description=panixy core & UI auto-upgrade (oneshot)
After=network-online.target

[Service]
Type=oneshot
ExecStart={{.Cli}} upgrade
