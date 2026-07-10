#!/bin/sh
# add-reflector: arm each NAT'd peer's nat module by reflecting its public endpoint.
#   add-reflector [--reflector <host>] [--vm <peer>]...   (default: reflector; peers node1 node2)
#
# why: masquerade NAT hides a node's public address from itself; astrald learns it only
#   when a directly-reachable peer observes the SNAT'd source and reflects it back
#   (reflectLink -> ObservedEndpointMessage, accepted only for a public tcp/utp endpoint).
# why: two masqueraded peers can't reflect each other pre-punch, so a non-NAT'd reflector does.
# note: run AFTER enter-nat, so the reflected source is the peer's public alias, not private 192.168.99.2.
set -eu

REFL="reflector"; PEERS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --reflector) [ $# -ge 2 ] || { echo "need host after --reflector" >&2; exit 64; }; REFL=$2; shift 2 ;;
    --vm)        [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; PEERS="${PEERS:+$PEERS }$2"; shift 2 ;;
    *) echo "usage: add-reflector [--reflector <host>] [--vm <peer>]..." >&2; exit 64 ;;
  esac
done
[ -n "$PEERS" ] || PEERS="node1 node2"

# 1) give the reflector a public alias and read its node identity
REFL_SETUP=$(cat <<'EOS'

set -eu
lan=$(ip -o -4 addr show | awk '$4 ~ /^10\.77\./ {print $2; exit}')
[ -n "$lan" ] || { echo "add-reflector: no 10.77 LAN nic on $(hostname)" >&2; exit 1; }
oct=$(ip -o -4 addr show dev "$lan" | awk '$4 ~ /^10\.77\./ {n=$4; sub(/\/.*/,"",n); split(n,a,"."); print a[4]; exit}')
pub="198.51.100.$oct"
ip addr add "$pub/24" dev "$lan" 2>/dev/null || true
# the reflector's own node identity (host sees the local anonymous caller as the node)
rid=$(astral-query apphost.whoami -out json 2>/dev/null | python3 -c '
import json,sys
for ln in sys.stdin:
    ln=ln.strip()
    if not ln: continue
    try: o=json.loads(ln)
    except Exception: continue
    v=o.get("Object")
    if isinstance(v,str) and len(v)>=64: print(v); break
    if isinstance(v,dict) and isinstance(v.get("Identity"),str): print(v["Identity"]); break')
[ -n "$rid" ] || { echo "add-reflector: could not read reflector identity via apphost.whoami on $(hostname)" >&2; exit 1; }
echo "$pub $rid"   # LAST stdout line: <public-addr> <identity-hex>
EOS
)
echo "add-reflector: configuring reflector on $REFL ..." >&2
out=$(netsim ssh "$REFL" -- "$REFL_SETUP" | tail -n1)
REFL_PUB=$(echo "$out" | awk '{print $1}')
REFL_ID=$(echo "$out" | awk '{print $2}')
case "$REFL_PUB" in 198.51.100.*) : ;; *) echo "add-reflector: bad reflector pub '$REFL_PUB' (out: $out)" >&2; exit 1 ;; esac
[ -n "$REFL_ID" ] || { echo "add-reflector: no reflector identity (out: $out)" >&2; exit 1; }
echo "add-reflector: reflector '$REFL' at tcp:$REFL_PUB:1791  id=$REFL_ID" >&2

# 2) seed each peer with the reflector endpoint and force a tcp link so it gets reflected
# note: force link -> reflector observes peer's public source -> peer's nat arms
for p in $PEERS; do
  echo "add-reflector: linking $p -> reflector (for endpoint reflection) ..." >&2
  # shellcheck disable=SC2029
  # why: peer's astrald is in netns priv; astral-query defaults to tcp:127.0.0.1:8625 (netns-local), so run it inside the netns
  netsim ssh "$p" -- "
    ip netns exec priv astral-query nodes.add_endpoint -id '$REFL_ID' -endpoint 'tcp:$REFL_PUB:1791' >/dev/null 2>&1 || true
    ip netns exec priv astral-query dir.set_alias   -id '$REFL_ID' -alias reflector             >/dev/null 2>&1 || true
    ip netns exec priv astral-query nodes.new_link  -target '$REFL_ID' -endpoint 'tcp:$REFL_PUB:1791' -out json 2>&1 | tail -3
  " || echo "add-reflector: WARNING new_link to reflector failed on $p (bring-up diagnoses)" >&2
done
echo "add-reflector: done (reflector=$REFL id=$REFL_ID pub=$REFL_PUB; peers: $PEERS)"
