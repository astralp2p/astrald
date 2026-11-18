#!/bin/sh
# restart-astrald: restart astrald on <vm> --in seconds from now, while the driver watches.
#   restart-astrald [--vm <host>] [--in <seconds>]     (default: node2, 30)
#
# why scheduled in the guest: a vmop runs before the driver and ends, and the driver has to be
#   watching when the node goes down and comes back. A transient systemd timer on <vm>
#   restarts astrald after --in seconds, as lan-outage schedules a return.
# why systemctl: install-astrald runs astrald as the systemd unit astrald, and a VM world
#   cannot restart a node through the manifest's restart key.
# note: the node keeps its state and config; only the process starts again.
set -eu

VM="node2"; IN=30
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --in) [ $# -ge 2 ] || { echo "need seconds after --in" >&2; exit 64; }; IN=$2; shift 2 ;;
    *)    echo "usage: restart-astrald [--vm <host>] [--in <seconds>]" >&2; exit 64 ;;
  esac
done
case "$IN" in ''|*[!0-9]*) echo "restart-astrald: --in takes whole seconds, not '$IN'" >&2; exit 64 ;; esac

netsim ssh "$VM" -- "set -eu
systemctl stop e2e-astrald-restart.timer 2>/dev/null || true
systemctl reset-failed e2e-astrald-restart.service e2e-astrald-restart.timer 2>/dev/null || true
systemd-run --quiet --unit=e2e-astrald-restart --on-active='${IN}s' /bin/systemctl restart astrald"
echo "restart-astrald: astrald on $VM restarts in ${IN}s"
