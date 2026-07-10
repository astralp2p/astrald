#!/bin/sh
# punch-nat: trigger astrald's NAT hole-punch between two NAT'd peers, leaving them with a
# direct kcp link. Final step of the nat-punch line (sibling of link-over-tor).
#
# Preconditions (the nat-punch story order): both peers behind a symmetric true-masquerade
# NAT (enter-nat: astrald in netns priv, port-preserving SNAT to 198.51.100.<oct>), nat-armed
# by reflection (add-reflector), and Tor relocated INTO the netns with WAN egress
# (configure-nat-tor). The punch's nat.node_punch signaling + peerSupportsNAT discovery route
# over a Tor link node1<->node2 (source-verified: tcp-only Basic strategy can't form for
# symmetric NAT, and the punch client sets no relay hint -> Tor is the sole mutual transport).
# On success the punch is promoted to a direct kcp link on BOTH peers (verify.py asserts it).
#
# Trigger is `nodes.new_link -strategies nat` (drives NATLinkStrategy end-to-end), NOT
# `nat.punch` (which only registers a Hole and yields no kcp link). Every astral-query targets
# a NAT'd node -> runs inside its netns (astral-query defaults to tcp:127.0.0.1:8625, which is
# netns-local; see enter-nat's header).
#   punch-nat [--vm <initiator>] [--peer <target>]   (default: node1 punches to node2)
set -eu

VM=node1; PEER=node2
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --peer) [ $# -ge 2 ] || { echo "need host after --peer" >&2; exit 64; }; PEER=$2; shift 2 ;;
    *) echo "usage: punch-nat [--vm <initiator>] [--peer <target>]" >&2; exit 64 ;;
  esac
done

# resolve _lib two dirs up from this script (as the verifiers do)
# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
LIB="$(dirname -- "$here")/_lib"

# --- host-side helpers (astral-query runs in the target's netns; parsed on the host
# via _lib/astralq.py -> the same astral-py interrogators the verifiers use) ------
nid() {  # a node's own identity hex (>=64 hex) via apphost.whoami
  netsim ssh "$1" -- "ip netns exec priv astral-query apphost.whoami -out json" 2>/dev/null \
    | python3 "$LIB/astralq.py" identity
}
onion_of() {  # a node's own .onion via resolve_endpoints localnode
  netsim ssh "$1" -- "ip netns exec priv astral-query nodes.resolve_endpoints -id localnode -out json" 2>/dev/null \
    | python3 "$LIB/astralq.py" onion
}
has_link() {  # <vm> <network> <identity> -> prints "yes" if that link exists
  netsim ssh "$1" -- "ip netns exec priv astral-query nodes.links -out json" 2>/dev/null \
    | python3 "$LIB/astralq.py" has-link "$2" "$3"
}
diag() {  # per-peer failure diagnosis (see the task doc "live_diagnostics")
  for v in "$VM" "$PEER"; do
    echo "--- diag $v ---" >&2
    netsim ssh "$v" -- '
      echo "[nodes.links]";   ip netns exec priv astral-query nodes.links -out json 2>&1 | tail -20
      echo "[nat.list_holes]"; ip netns exec priv astral-query nat.list_holes -out json 2>&1 | tail -5
      echo "[public_ip]";     ip netns exec priv astral-query ip.public_ip_candidates -out json 2>&1 | tail -5
      echo "[tor ctl 9051]";  ip netns exec priv ss -ltn 2>/dev/null | grep 9051 || echo none
      echo "[conntrack 198.51.100]"; (conntrack -L -p udp 2>/dev/null | grep 198.51.100 || grep 198.51.100 /proc/net/nf_conntrack 2>/dev/null) | head -6
      echo "[astrald journal]"; journalctl -u astrald --no-pager 2>&1 | tail -40
    ' >&2 2>&1 || true
  done
}

echo "punch-nat: resolving identities ($VM initiator -> $PEER target) ..."
VMID=$(nid "$VM");     [ -n "$VMID" ]   || { echo "punch-nat: could not resolve $VM identity" >&2; exit 1; }
PEERID=$(nid "$PEER"); [ -n "$PEERID" ] || { echo "punch-nat: could not resolve $PEER identity" >&2; exit 1; }

# 1) ensure mutual onion knowledge (host-brokered; do NOT trust auto-sync -- risk per doc)
O_PEER=$(onion_of "$PEER"); O_VM=$(onion_of "$VM")
[ -n "$O_PEER" ] || { echo "punch-nat: $PEER published no onion (Tor-in-netns down? run configure-nat-tor)" >&2; diag; exit 1; }
[ -n "$O_VM" ]   || { echo "punch-nat: $VM published no onion (Tor-in-netns down? run configure-nat-tor)" >&2; diag; exit 1; }
netsim ssh "$VM"   -- "ip netns exec priv astral-query nodes.add_endpoint -id '$PEERID' -endpoint 'tor:$O_PEER' >/dev/null 2>&1 || true"
netsim ssh "$PEER" -- "ip netns exec priv astral-query nodes.add_endpoint -id '$VMID'   -endpoint 'tor:$O_VM'   >/dev/null 2>&1 || true"
echo "punch-nat: seeded onions ($VM<->$PEER)"

# 2) readiness: a live tor signaling link $VM->$PEER (form one if absent; ~60s bound)
tor_up=
for _ in $(seq 1 20); do
  [ "$(has_link "$VM" tor "$PEERID")" = yes ] && { tor_up=1; break; }
  netsim ssh "$VM" -- "timeout 60 ip netns exec priv astral-query nodes.new_link -target '$PEERID' -strategies tor -out json >/dev/null 2>&1 || true"
  sleep 3
done
[ -n "$tor_up" ] || { echo "punch-nat: no tor link $VM->$PEER (signaling path down)" >&2; diag; exit 1; }
echo "punch-nat: tor signaling link up ($VM->$PEER)"

# 3) trigger the punch (initiator only; node2's side runs automatically over nat.node_punch)
echo "punch-nat: triggering NAT punch $VM -> $PEER ..."
netsim ssh "$VM" -- "timeout 180 ip netns exec priv astral-query nodes.new_link -target '$PEERID' -strategies nat -out json 2>&1 | tail -3" || true

# 4) confirm a durable kcp link on BOTH peers (~60s bound)
ok=
for _ in $(seq 1 20); do
  if [ "$(has_link "$VM" kcp "$PEERID")" = yes ] && [ "$(has_link "$PEER" kcp "$VMID")" = yes ]; then ok=1; break; fi
  sleep 3
done
[ -n "$ok" ] || { echo "punch-nat: no kcp link between $VM and $PEER after the punch" >&2; diag; exit 1; }
echo "punch-nat: kcp link established ($VM<->$PEER); done"
