#!/bin/sh
[ -d /run/systemd/system ] || exit 0
systemctl daemon-reload >/dev/null 2>&1 || true
systemctl try-restart pangolin-client.service >/dev/null 2>&1 || true
exit 0
