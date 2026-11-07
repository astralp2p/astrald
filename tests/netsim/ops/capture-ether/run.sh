#!/bin/sh
# capture-ether: capture every ether datagram (UDP 8822) a VM sends or hears, for --for
#   seconds, and hand the record to the node through its own tree.
#   capture-ether [--vm <host>] [--for <seconds>] [--name <label>]   (default: reflector, 150, ether)
#
# why in the guest: a capture on the VM's own interfaces sees every datagram the VM sends,
#   on the LAN and the WAN alike, which no peer can; tcpdump ships with the lab's Ubuntu
#   cloud image.
# why the capture outlives this op: a vmop runs before the driver and ends, and what the
#   capture has to see is what the driver makes happen. It runs as a transient systemd unit.
# why the record goes into the node's tree: a driver reaches a guest only through its
#   apphost (tests/README.md, Drivers), so when its window closes the unit writes the record
#   to /e2e/capture/<label> with the guest's astral-query, as the node, and the driver reads
#   it with tree.get. /e2e is no module's path.
# note: the unit takes the node's token from this ssh session, where vm.py's sshd drop-in
#   sets ASTRALD_APPHOST_TOKEN for root; a unit PID 1 starts inherits no session environment.
# note: returns only once tcpdump listens, so nothing the driver does escapes the capture.
set -eu

VM="reflector"; FOR=150; NAME="ether"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --for)  [ $# -ge 2 ] || { echo "need seconds after --for" >&2; exit 64; }; FOR=$2; shift 2 ;;
    --name) [ $# -ge 2 ] || { echo "need a label after --name" >&2; exit 64; }; NAME=$2; shift 2 ;;
    *) echo "usage: capture-ether [--vm <host>] [--for <seconds>] [--name <label>]" >&2; exit 64 ;;
  esac
done
case "$FOR" in ''|*[!0-9]*) echo "capture-ether: --for takes whole seconds, not '$FOR'" >&2; exit 64 ;; esac
case "$NAME" in ''|*[!a-z0-9-]*) echo "capture-ether: --name takes [a-z0-9-], not '$NAME'" >&2; exit 64 ;; esac

HERE=${NETSIM_TASK_DIR:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)}
D=/var/lib/astral-tests

netsim ssh "$VM" -- "install -d -m 700 $D && cat > $D/capture-$NAME.py" < "$HERE/capture.py"

# shellcheck disable=SC2029
netsim ssh "$VM" -- "set -eu
command -v tcpdump >/dev/null || { echo 'capture-ether: no tcpdump in the guest' >&2; exit 1; }
[ -n \"\${ASTRALD_APPHOST_TOKEN:-}\" ] || { echo 'capture-ether: no node token in the session' >&2; exit 1; }
rm -f $D/capture-$NAME.ready $D/capture-$NAME.json
systemctl stop e2e-capture-$NAME 2>/dev/null || true
systemctl reset-failed e2e-capture-$NAME 2>/dev/null || true
systemd-run --quiet --unit=e2e-capture-$NAME --collect -E ASTRALD_APPHOST_TOKEN \
  python3 $D/capture-$NAME.py --for $FOR --name $NAME
n=0
while [ ! -e $D/capture-$NAME.ready ]; do
  n=\$((n + 1)); [ \$n -le 150 ] || { echo 'capture-ether: tcpdump never listened' >&2; exit 1; }
  sleep 0.1
done"
echo "capture-ether: capturing UDP 8822 on $VM for ${FOR}s; record at /e2e/capture/$NAME"
