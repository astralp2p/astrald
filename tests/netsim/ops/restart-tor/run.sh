#!/bin/sh
# restart-tor: restart Tor on <vm> --in seconds from now, while the driver watches; with --down,
#   stop Tor at --in and start it again --down seconds after the stop.
#   restart-tor [--vm <host>] [--in <seconds>] [--down <seconds>]   (default: node2, 30, 0)
#
# why scheduled in the guest: a vmop runs before the driver and ends, and the driver has to be
#   watching when Tor goes down and comes back. One transient systemd timer on <vm> stops Tor,
#   waits --down s and starts it, as restart-astrald schedules a restart.
# why one timer, with AccuracySec=1s: a transient timer keeps systemd's default AccuracySec of
#   1 min, and two timers --down s apart can then fire together or out of order. One unit
#   running stop, sleep, start holds Tor down for --down s from the stop's end, and the 1 s
#   accuracy fires it --in s on (systemd-run(1), the AccuracySec example).
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

REMOTE_BODY=$(cat <<'EOS'
set -eu
systemctl is-active --quiet tor || { echo "restart-tor: tor is not running on $(hostname); run enable-tor first" >&2; exit 1; }
units=tor.service
if systemctl cat tor@default.service >/dev/null 2>&1; then
  units="tor@default.service tor.service"
fi
systemctl stop e2e-tor-restart.timer e2e-tor-restart.service 2>/dev/null || true
systemctl reset-failed e2e-tor-restart.service e2e-tor-restart.timer 2>/dev/null || true
if [ "$down_s" = 0 ]; then
  action="/bin/systemctl restart $units"
  echo "restart-tor: $(hostname) restarts $units in ${in_s}s" >&2
else
  # note: ';', not '&&': Tor starts again even if the stop reports a failure
  action="/bin/systemctl stop $units; /bin/sleep $down_s; /bin/systemctl start $units"
  echo "restart-tor: $(hostname) stops $units in ${in_s}s and starts them ${down_s}s after the stop" >&2
fi
systemd-run --quiet --unit=e2e-tor-restart --on-active="${in_s}s" \
  --timer-property=AccuracySec=1s /bin/sh -c "$action"
EOS
)
# shellcheck disable=SC2029
netsim ssh "$VM" -- "in_s='$IN'; down_s='$DOWN'
$REMOTE_BODY"
if [ "$DOWN" = 0 ]; then
  echo "restart-tor: Tor on $VM restarts in ${IN}s"
else
  echo "restart-tor: Tor on $VM stops in ${IN}s and starts again ${DOWN}s after the stop"
fi
