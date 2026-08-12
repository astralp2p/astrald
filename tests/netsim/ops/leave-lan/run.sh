#!/bin/sh
# leave-lan: make <vm> genuinely leave the 10.77 LAN so astrald re-links to <peer> over Tor.
#   leave-lan [--vm <host>] [--peer <host>]    (default: node2 leaves, peer node1)
#
# why: seed <peer> with <vm>'s onion while the LAN is up; once it's gone the peer can no
#   longer ask <vm> for its address, so it needs the .onion cached first.
# why: withdraw <vm>'s own 10.77 address (ip addr flush) to leave. astrald has no carrier
#   monitor: it polls net.InterfaceAddrs() every 3s, one tcp endpoint per assigned IP, so
#   removing the address is what it observes as "left the network" -> drops the 10.77
#   endpoint, re-links over Tor. A DROP or link-down leaves the IPv4 address, invisible to it.
# note: SSH/management rides the separate WAN NIC, untouched.
# note: both nodes need Tor up (enable-tor) and <vm> must resolve on <peer> (adopt-node);
#   the astral-query ops here (resolve_endpoints / add_endpoint) are ungated.
set -eu

VM="node2"; PEER="node1"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --peer) [ $# -ge 2 ] || { echo "need host after --peer" >&2; exit 64; }; PEER=$2; shift 2 ;;
    *)      echo "usage: leave-lan [--vm <host>] [--peer <host>]" >&2; exit 64 ;;
  esac
done

# 1) seed <peer> with <vm>'s onion before the LAN goes away
#
# why host-side: the peer cannot learn the onion for itself. Its endpoint cache
# holds only the leaver's tcp/kcp addresses (tor endpoints do not sync over the
# link), and asking the leaver directly — `<leaver>:nodes.resolve_endpoints` —
# answers `route_not_found`. Both were verified against a live pair. The host
# runs this vmop and can read either guest, so it fetches the onion from the
# leaver and hands it to the peer.
#
# why the hex identity: nodes.add_endpoint takes `id` of type identity and
# rejects an alias with `query rejected (1)`.
#
# why ASTRAL_ID_<vm> first: this used to find the identity by searching the
# peer's roster for the alias `node2`. Only adopt-node's script.py sets that
# alias — the agent prompt says nothing about aliases, correctly, because an
# alias is harness bookkeeping and not a thing a user asks for. So this step
# passed script-driven and failed agent-driven with "does not know node2 in
# its swarm roster" while the roster held node2 all along. The harness knows
# every identity and exports it; the roster is then checked BY IDENTITY, which
# keeps the real precondition (the peer knows the leaver) without depending on
# whether anyone bothered to name it.
echo "leave-lan: seeding $PEER with $VM's onion ..."

onion=$(netsim ssh "$VM" -- "python3 -c \"import json;print(json.load(open('/root/tor.json'))['onion'])\"" 2>/dev/null | tr -d '\r\n')
[ -n "$onion" ] || { echo "leave-lan: $VM has no onion in /root/tor.json (run enable-tor first)" >&2; exit 1; }

# the harness's value when it ran us; the alias hunt remains for a hand-run vmop
eval "leaver_id=\${ASTRAL_ID_$VM:-}"
roster=$(netsim ssh "$PEER" -- "astral-query user.swarm_status -out json" 2>/dev/null)
leaver_id=$(printf '%s\n' "$roster" | python3 -c "
import json,sys
want, alias = sys.argv[1], sys.argv[2]
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    try: o = json.loads(line)
    except Exception: continue
    m = (o.get('Object') or {})
    ident = m.get('Identity')
    if not ident: continue
    # the harness told us who; this confirms the peer knows them. With no
    # harness value (a hand-run vmop) fall back to the alias.
    if (ident == want) if want else (m.get('Alias') == alias):
        print(ident); break
" "$leaver_id" "$VM" | tr -d '\r\n')
[ -n "$leaver_id" ] || { echo "leave-lan: $PEER does not know $VM in its swarm roster" >&2; exit 1; }

netsim ssh "$PEER" -- "astral-query nodes.add_endpoint -id $leaver_id -endpoint 'tor:$onion'" >/dev/null 2>&1 \
  || { echo "leave-lan: $PEER refused the endpoint for $VM" >&2; exit 1; }
echo "leave-lan: $PEER seeded $VM ($leaver_id) onion=$onion"

# 2) make <vm> leave the LAN: withdraw its own 10.77 address (drop the NIC too, for realism)
# why: flushing the address takes its connected /24 route with it -> <vm> has no LAN address
#   or route, genuinely gone at the IP layer, which is what astrald observes (see header).
CUT_BODY=$(cat <<'EOS'
set -eu
# the NIC holding the 10.77 LAN address is nic2; SSH rides the separate WAN NIC, untouched.
lan_if=$(ip -o -4 addr show | awk '$4 ~ /^10\.77\./ {print $2; exit}')
[ -n "$lan_if" ] || { echo "leave-lan: no 10.77 LAN interface on $(hostname)" >&2; exit 1; }
lan_ip=$(ip -o -4 addr show dev "$lan_if" | awk '$4 ~ /^10\.77\./ {print $4; exit}')
ip addr flush dev "$lan_if"   # RTM_DELADDR: drops the address AND its connected /24 route
ip link set "$lan_if" down    # carrier/admin down too, so the NIC is faithfully "gone"
echo "leave-lan: $(hostname) withdrew $lan_ip from $lan_if (left the LAN)"
EOS
)
echo "leave-lan: $VM leaving the LAN (withdrawing its 10.77 address) ..."
# shellcheck disable=SC2029
netsim ssh "$VM" -- "$CUT_BODY"
echo "leave-lan: done on $VM"
