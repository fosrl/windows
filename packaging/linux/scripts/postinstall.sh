#!/bin/sh
# Runs after install and upgrade. Arguments differ by format:
#   deb: configure <old version, empty on a new install>
#   rpm: 1 on install, 2 or more on upgrade
#   Arch post_install: <new version> (Arch upgrades run postupgrade-arch.sh)
[ -d /run/systemd/system ] || exit 0

upgrade=0
case "$1" in
    configure) [ -n "$2" ] && upgrade=1 ;;
    [2-9]) upgrade=1 ;;
esac

systemctl daemon-reload >/dev/null 2>&1 || true
if [ "$upgrade" = 1 ]; then
    # The in-app updater runs this in its own unit and expects the restart.
    systemctl try-restart pangolin-client.service >/dev/null 2>&1 || true
elif [ -f /etc/arch-release ]; then
    echo "Start the Pangolin background service with:"
    echo "  sudo systemctl enable --now pangolin-client"
else
    systemctl enable --now pangolin-client.service >/dev/null 2>&1 || true
fi
exit 0
