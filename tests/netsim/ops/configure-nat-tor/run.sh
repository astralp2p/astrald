#!/bin/sh
# configure-nat-tor: relocate a NAT'd node's Tor into its netns priv so astrald regains
# full Tor -- inbound AND outbound onion -- after enter-nat moved it into that netns.
#   configure-nat-tor [--vm <host>]...     (default: node1 node2)
#
# why: astrald's tor module reaches Tor at 127.0.0.1:9050 (SOCKS) / 127.0.0.1:9051 (control),
#   and its onion listener is hardcoded 127.0.0.1:0 (mod/tor/src/server.go) that Tor dials
#   inbound; once astrald is in netns priv that 127.0.0.1 is the netns loopback, so root-ns
#   Tor can neither be reached for SOCKS/control nor deliver inbound onion. Fix (no astrald
#   change): run Tor inside the same netns, give the netns WAN egress to reach the Tor network.
# note: WAN masquerade carries only Tor's internet path (192.168.99.0/24); enter-nat's LAN
#   SNAT to 198.51.100.x still handles peer traffic -- routing splits by destination.
# note: run AFTER enable-tor (Tor + control port) and enter-nat (netns + astrald in it).
set -eu

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    *)    echo "usage: configure-nat-tor [--vm <host>]..." >&2; exit 64 ;;
  esac
done
[ -n "$VMS" ] || VMS="node1 node2"

REMOTE_BODY=$(cat <<'EOS'
set -eu

# preconditions from enter-nat / enable-tor
ip netns list 2>/dev/null | grep -qw priv \
  || { echo "configure-nat-tor: netns priv missing on $(hostname) (run enter-nat first)" >&2; exit 1; }
systemctl cat tor@default.service >/dev/null 2>&1 \
  || { echo "configure-nat-tor: tor@default.service not found on $(hostname) (run enable-tor first)" >&2; exit 1; }

# 1) WAN egress for the netns so Tor-in-netns can reach the real Tor network. The slirp WAN
#    NIC is the default-route interface (it keeps its kernel name; only lan0 is renamed).
wan=$(ip route show default | awk '{print $5; exit}')
[ -n "$wan" ] || { echo "configure-nat-tor: no default route / WAN nic on $(hostname)" >&2; exit 1; }
# idempotent append to enter-nat's existing ip/nat postrouting chain (keeps the LAN SNAT).
nft list chain ip nat postrouting 2>/dev/null | grep -q "oifname \"$wan\" masquerade" \
  || nft add rule ip nat postrouting ip saddr 192.168.99.0/24 oifname "$wan" masquerade

# 2) move the Tor daemon into netns "priv" (Debian runs it as tor@default.service; the
#    tor.service wrapper pulls it in). Same NetworkNamespacePath idiom enter-nat used for
#    astrald -- joins only the NET ns, so torrc (mount ns) is untouched.
mkdir -p /etc/systemd/system/tor@default.service.d
cat > /etc/systemd/system/tor@default.service.d/netns.conf <<UNIT
[Service]
NetworkNamespacePath=/run/netns/priv
UNIT
systemctl daemon-reload
systemctl restart tor@default.service

# 3) wait for Tor's control port to open INSIDE the netns (ss must run in the netns).
ok=
for _ in $(seq 1 30); do
  if ip netns exec priv ss -ltn 2>/dev/null | grep -q '127.0.0.1:9051'; then ok=1; break; fi
  sleep 1
done
[ -n "$ok" ] || {
  echo "configure-nat-tor: tor control 9051 did not open in netns priv on $(hostname)" >&2
  journalctl -u tor@default --no-pager 2>&1 | tail -20 >&2 || true
  exit 1
}

# 4) restart astrald (already in the netns) so its tor module re-inits against the now
#    netns-local control port, then confirm it re-publishes an onion. Success here proves
#    Tor-in-netns end to end: bootstrap via the WAN masquerade + control + the onion local
#    listener are ALL netns-local. astrald's onion key persists under -root (shared mount
#    ns), so it comes back as the same onion.
systemctl restart astrald
onion=
for _ in $(seq 1 90); do
  if systemctl is-active --quiet astrald; then
    # astrald is in netns "priv"; astral-query defaults to tcp:127.0.0.1:8625 (netns-local).
    onion=$(ip netns exec priv astral-query nodes.resolve_endpoints -id localnode -out json 2>/dev/null | python3 -c '
import json,sys
def addr(ep):
    if isinstance(ep, str): return ep
    if isinstance(ep, dict):
        o = ep.get("Object"); return o if isinstance(o, str) else ""
    return ""
for ln in sys.stdin:
    ln = ln.strip()
    if not ln: continue
    try: o = json.loads(ln)
    except Exception: continue
    a = addr((o.get("Object") or {}).get("Endpoint"))
    if ".onion" in a: print(a); break')
    [ -n "$onion" ] && break
  fi
  sleep 2
done
[ -n "$onion" ] || {
  echo "configure-nat-tor: astrald did not re-publish a tor onion in netns on $(hostname)" >&2
  journalctl -u tor@default --no-pager 2>&1 | tail -20 >&2 || true
  journalctl -u astrald     --no-pager 2>&1 | tail -20 >&2 || true
  exit 1
}
echo "configure-nat-tor: $(hostname) Tor now in netns priv (onion=$onion, wan=$wan)"
EOS
)

# why: $VMS is a space-separated list -> word-splitting is intentional
# shellcheck disable=SC2086
for vm in $VMS; do
  echo "configure-nat-tor: relocating Tor into $vm's netns ..."
  # shellcheck disable=SC2029
  netsim ssh "$vm" -- "$REMOTE_BODY"
done
echo "configure-nat-tor: done ($VMS)"
