#!/bin/sh
# lan-outage: take <vm> off the 10.77 LAN now, and bring it back at the same address after
#   --for seconds.
#   lan-outage [--vm <host>] [--for <seconds>]     (default: node2, 420)
#
# why the return is scheduled in the guest: a vmop runs before the driver and ends, and the
#   driver has to be watching when the node comes back. A transient systemd timer on <vm>
#   restores the address while the driver watches.
# why the same address: a node back at the address its peers last heard it from is one nearby
#   notices by expiry alone (PeerSeen for a node back after its 5 min cache entry expired),
#   which is the case nearby-relink judges.
# why it leaves as leave-lan does: astrald keys "left the network" on the address (it polls
#   net.InterfaceAddrs()), so the address is withdrawn and the NIC taken down; its return is
#   then an address change, on which nearby sends its status and scans at once.
# why the unreachable route: with its LAN address gone, <vm>'s own dials to its peers would
#   leave by the WAN default route through slirp and wait out a connect timeout; the route
#   fails each at once, so its link tasks back off as a node off the network does.
# why links are closed on every VM: withdrawing an address disturbs no established
#   connection, and both ends would go on listing a link nothing carries (leave-lan, step 3).
#   Closing them is what makes each side's link task see the drop and start backing off.
# note: SSH rides the separate WAN NIC, so the harness reaches <vm> and its apphost throughout.
set -eu

VM="node2"; FOR=420
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)  [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --for) [ $# -ge 2 ] || { echo "need seconds after --for" >&2; exit 64; }; FOR=$2; shift 2 ;;
    *)     echo "usage: lan-outage [--vm <host>] [--for <seconds>]" >&2; exit 64 ;;
  esac
done
case "$FOR" in ''|*[!0-9]*) echo "lan-outage: --for takes whole seconds, not '$FOR'" >&2; exit 64 ;; esac

# 1) schedule the return, then leave
CUT_BODY=$(cat <<'EOS'
set -eu
lan_if=$(ip -o -4 addr show | awk '$4 ~ /^10\.77\./ {print $2; exit}')
[ -n "$lan_if" ] || { echo "lan-outage: no 10.77 LAN interface on $(hostname)" >&2; exit 1; }
lan_cidr=$(ip -o -4 addr show dev "$lan_if" | awk '$4 ~ /^10\.77\./ {print $4; exit}')
lan_net=$(python3 -c 'import ipaddress,sys; print(ipaddress.ip_interface(sys.argv[1]).network)' "$lan_cidr")
systemctl stop e2e-lan-return.timer 2>/dev/null || true
systemctl reset-failed e2e-lan-return.service e2e-lan-return.timer 2>/dev/null || true
# why first: the node comes back even if the cut below stops half way
# note: replace, not add: networkd may set the address itself once the link is up
systemd-run --quiet --unit=e2e-lan-return --on-active="${for_s}s" /bin/sh -c \
  "ip route del unreachable $lan_net 2>/dev/null; ip link set $lan_if up; ip addr replace $lan_cidr dev $lan_if"
ip addr flush dev "$lan_if"
ip link set "$lan_if" down
ip route replace unreachable "$lan_net"
echo "lan-outage: $(hostname) left the LAN ($lan_cidr on $lan_if) at $(date -u +%FT%T.%NZ), back in ${for_s}s" >&2
echo "${lan_cidr%/*}"
EOS
)
echo "lan-outage: $VM leaving the LAN for ${FOR}s ..."
lan_ip=$(netsim ssh "$VM" -- "for_s='$FOR'; $CUT_BODY" | tail -n1 | tr -d '\r\n')
case "$lan_ip" in 10.77.*) : ;; *) echo "lan-outage: $VM reported no LAN address ('$lan_ip')" >&2; exit 1 ;; esac

# 2) close every link that rode the address that left, on every VM
CLOSE_BODY=$(cat <<'EOS'
set -eu
astral-query nodes.links -out json 2>/dev/null | python3 -c "
import json, os, re, subprocess, sys
gone = re.compile('(?<![0-9.])' + re.escape(os.environ['gone_ip']) + '(?![0-9])')
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        o = json.loads(line)
    except Exception:
        continue
    m = o.get('Object') or {}
    if not isinstance(m, dict) or not m.get('ID'):
        continue
    if not gone.search(json.dumps([m.get('LocalEndpoint'), m.get('RemoteEndpoint')])):
        continue
    subprocess.run(['astral-query', 'nodes.close_link', '-link_id', str(m['ID'])],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    print('closed %s link %s' % (m.get('Network', '?'), m['ID']))
"
EOS
)
VMS=$(netsim vm ls --json | python3 -c \
  'import json,sys; print(" ".join(v["hostname"] for v in json.load(sys.stdin) if v["state"]=="running"))')
# shellcheck disable=SC2086
for host in $VMS; do
  # shellcheck disable=SC2029
  out=$(netsim ssh "$host" -- "export gone_ip='$lan_ip'; $CLOSE_BODY" 2>/dev/null || true)
  [ -n "$out" ] && echo "$out" | sed "s/^/lan-outage: $host /"
done
echo "lan-outage: $VM ($lan_ip) is off the LAN; e2e-lan-return brings it back in ${FOR}s"
