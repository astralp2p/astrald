#!/bin/sh
# stop-astrald: stop astrald on each named VM and leave it stopped.
#   stop-astrald <vm>...
#
# why an op: a VM world has no `stop` step (tests/README.md, `stop` runs in
#   local and Docker worlds only), and a node cannot stop itself over apphost.
# note: the VM stays up, so sshd, the harness's apphost tunnel and the
#   journal stay reachable; a query through the tunnel is closed at once.
set -eu

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    *) echo "usage: stop-astrald <vm>..." >&2; exit 64 ;;
  esac
done
[ -n "$VMS" ] || { echo "stop-astrald: name at least one VM" >&2; exit 64; }

# shellcheck disable=SC2086
for vm in $VMS; do
  netsim ssh "$vm" -- "systemctl stop astrald"
  echo "stop-astrald: astrald stopped on $vm"
done
