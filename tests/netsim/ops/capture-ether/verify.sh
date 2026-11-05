#!/bin/sh
# verify capture-ether (same args as run.sh): the capture unit runs on the VM and tcpdump
# listens. INDEPENDENT re-check of the guest; what the capture records is the oracle's.
set -eu
VM="reflector"; NAME="ether"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   VM=$2; shift 2 ;;
    --name) NAME=$2; shift 2 ;;
    *)      shift ;;
  esac
done
# shellcheck disable=SC2029
netsim ssh "$VM" -- "systemctl is-active --quiet e2e-capture-$NAME && pgrep -x tcpdump >/dev/null" \
  || { echo "capture-ether verify FAILED: no capture running on $VM" >&2; exit 1; }
echo "capture-ether OK: e2e-capture-$NAME runs tcpdump on $VM"
