[Unit]
Description=Daily panixy upgrade check at 04:17 (+0-25min jitter)

[Timer]
OnCalendar=*-*-* 04:17:00
RandomizedDelaySec=25m
Persistent=true

[Install]
WantedBy=timers.target
