#!/bin/sh
# restart-tor: restart Tor on <vm> --in seconds from now, while the driver watches; with --down,
#   stop Tor at --in and start it again --down seconds later.
#   restart-tor [--vm <host>] [--in <seconds>] [--down <seconds>]   (default: node2, 30, 0)
#
# why scheduled in the guest: a vmop runs before the driver and ends, and the driver has to be
#   watching when Tor goes down and comes back. Transient systemd timers on <vm> stop and start
#   Tor, as restart-astrald schedules a restart.
# why Tor and not astrald: astrald's tor module owns its onion service through the control
#   connection, and Tor drops the service with that connection (mod/tor/src/server.go). A Tor
#   restart under a running astrald is what the onion's re-registration answers.
# why tor@default too: Debian and Ubuntu run the Tor daemon as the instance tor@default under
#   the multi-instance master tor.service. Both units are named, so the daemon itself stops and
#   starts whatever relation the guest's units hold; a guest without the instance names tor alone.
# note: astrald keeps running; only Tor stops. enable-tor runs first and installs Tor.
set -eu

VM="node2"; IN=30; DOWN=0
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --in)   [ $# -ge 2 ] || { echo "need seconds after --in" >&2; exit 64; }; IN=$2; shift 2 ;;
    --down) [ $# -ge 2 ] || { echo "need seconds after --down" >&2; exit 64; }; DOWN=$2; shift 2 ;;
    *)      echo "usage: restart-tor [--vm <host>] [--in <seconds>] [--down <seconds>]" >&2; exit 64 ;;
  esac
done
case "$IN" in ''|*[!0-9]*) echo "restart-tor: --in takes whole seconds, not '$IN'" >&2; exit 64 ;; esac
case "$DOWN" in ''|*[!0-9]*) echo "restart-tor: --down takes whole seconds, not '$DOWN'" >&2; exit 64 ;; esac
UP=$((IN + DOWN))

REMOTE_BODY=$(cat <<'EOS'
set -eu
systemctl is-active --quiet tor || { echo "restart-tor: tor is not running on $(hostname); run enable-tor first" >&2; exit 1; }
units=tor.service
if systemctl cat tor@default.service >/dev/null 2>&1; then
  units="tor@default.service tor.service"
fi
for u in e2e-tor-stop e2e-tor-start; do
  systemctl stop "$u.timer" 2>/dev/null || true
  systemctl reset-failed "$u.service" "$u.timer" 2>/dev/null || true
done
# shellcheck disable=SC2086
if [ "$down_s" = 0 ]; then
  systemd-run --quiet --unit=e2e-tor-stop --on-active="${in_s}s" /bin/systemctl restart $units
  echo "restart-tor: $(hostname) restarts $units in ${in_s}s" >&2
else
  systemd-run --quiet --unit=e2e-tor-stop --on-active="${in_s}s" /bin/systemctl stop $units
  systemd-run --quiet --unit=e2e-tor-start --on-active="${up_s}s" /bin/systemctl start $units
  echo "restart-tor: $(hostname) stops $units in ${in_s}s and starts them in ${up_s}s" >&2
fi
EOS
)
# shellcheck disable=SC2029
netsim ssh "$VM" -- "in_s='$IN'; down_s='$DOWN'; up_s='$UP'
$REMOTE_BODY"
if [ "$DOWN" = 0 ]; then
  echo "restart-tor: Tor on $VM restarts in ${IN}s"
else
  echo "restart-tor: Tor on $VM stops in ${IN}s and starts again in ${UP}s"
fi
