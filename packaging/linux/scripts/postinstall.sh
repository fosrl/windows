#!/bin/sh
# Runs after install and upgrade. Arguments differ by format:
#   deb: configure <old version, empty on a new install>
#   rpm: 1 on install, 2 or more on upgrade
#   Arch post_install: <new version> (Arch upgrades run postupgrade-arch.sh)
[ -d /run/systemd/system ] || exit 0

# preremove.sh leaves this when the package is removed. deb keeps passing the
# old version after "apt remove" (config files stay), so without it a reinstall
# would look like an upgrade and the service would stay off.
removed=/var/lib/pangolin-client/.removed

upgrade=0
case "$1" in
    configure) [ -n "$2" ] && upgrade=1 ;;
    [2-9]) upgrade=1 ;;
esac
[ -e "$removed" ] && upgrade=0
rm -f "$removed"

systemctl daemon-reload >/dev/null 2>&1 || true
if [ "$upgrade" = 1 ]; then
    # The in-app updater runs this in its own unit and expects the restart.
    systemctl try-restart pangolin-client.service >/dev/null 2>&1 || true
else
    systemctl enable --now pangolin-client.service >/dev/null 2>&1 || true
fi
exit 0
