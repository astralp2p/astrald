#!/bin/sh
# enter-nat: put a node's astrald behind its own symmetric true-masquerade NAT. Builds the
# NAT only; it does NOT punch.
#   enter-nat [--vm <host>]...      (default: node1 node2; one call NATs each peer)
#
# why: relocating astrald into netns priv severs its direct 10.77 LAN path, so the swarm
#   link maintainer re-links the pair over Tor -- the node is now a genuine NAT'd peer.
# note: per --vm: netns priv (192.168.99.2) via veth pair; port-preserving SNAT of
#   192.168.99.0/24 to public alias 198.51.100.<oct>; inbound DNAT of that alias into the
#   netns (else punch packets hit local INPUT and never complete, see nat table section);
#   astrald relaunched inside the netns via a systemd drop-in (same -root -> same identity).
# note: astral-query defaults to tcp:127.0.0.1:8625 (lib/apphost DefaultEndpoint; only the
#   TOKEN is env-overridable, not the endpoint); in netns priv that 127.0.0.1 is the netns
#   loopback, so EVERY astral-query against a NAT'd node must run inside the netns:
#   `ip netns exec priv astral-query ...` (see add-reflector / verify / configure-nat-tor).
# note: astrald can't see its own public alias (masquerade), so its nat module stays disabled
#   until the reflector reflects that endpoint back (add-reflector).
set -eu

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    *)    echo "usage: enter-nat [--vm <host>]..." >&2; exit 64 ;;
  esac
done
[ -n "$VMS" ] || VMS="node1 node2"

REMOTE_BODY=$(cat <<'EOS'
set -eu
export DEBIAN_FRONTEND=noninteractive
command -v nft >/dev/null 2>&1 || {
  apt-get -qq -o DPkg::Lock::Timeout=120 update
  apt-get -qq -y -o DPkg::Lock::Timeout=120 install nftables >/dev/null
}

# the LAN NIC carries the 10.77 address; its last octet indexes our public alias.
lan=$(ip -o -4 addr show | awk '$4 ~ /^10\.77\./ {print $2; exit}')
[ -n "$lan" ] || { echo "enter-nat: no 10.77 LAN interface on $(hostname)" >&2; exit 1; }
oct=$(ip -o -4 addr show dev "$lan" | awk '$4 ~ /^10\.77\./ {n=$4; sub(/\/.*/,"",n); split(n,a,"."); print a[4]; exit}')
[ -n "$oct" ] || { echo "enter-nat: could not read 10.77 octet on $(hostname)" >&2; exit 1; }
pub="198.51.100.$oct"
ip addr add "$pub/24" dev "$lan" 2>/dev/null || true

# private host 192.168.99.2 in netns "priv"; this VM is its only way out
ip netns add priv 2>/dev/null || true
ip link add veth0 type veth peer name veth0p 2>/dev/null || true
ip link set veth0p netns priv 2>/dev/null || true
ip addr add 192.168.99.1/24 dev veth0 2>/dev/null || true
ip link set veth0 up
ip -n priv addr add 192.168.99.2/24 dev veth0p 2>/dev/null || true
ip -n priv link set veth0p up; ip -n priv link set lo up
ip -n priv route replace default via 192.168.99.1
sysctl -wq net.ipv4.ip_forward=1
sysctl -wq net.ipv4.conf.all.rp_filter=2
sysctl -wq net.netfilter.nf_conntrack_udp_timeout=60 2>/dev/null || true
sysctl -wq net.netfilter.nf_conntrack_udp_timeout_stream=180 2>/dev/null || true

# port-preserving SNAT to the public alias (idempotent: rebuild the nat table)
nft add table ip nat 2>/dev/null || true
nft flush table ip nat
nft add chain ip nat postrouting '{ type nat hook postrouting priority 100 ; }'
nft add rule ip nat postrouting ip saddr 192.168.99.0/24 oifname "$lan" snat ip to "$pub"

# Inbound DNAT of the public alias into the netns -- REQUIRED for the punch to complete.
# The alias 198.51.100.<oct> is a LOCAL address on this box (NAT and endpoint are collapsed
# onto one VM), so inbound punch packets are delivered to the local INPUT (no listener) and
# never reach the netns puncher; the conntrack-reply reverse-SNAT that should forward them
# fails under a source-port-realloc clash (the inbound-to-local-alias creates a conntrack
# entry that collides with the outbound SNAT). DNAT the alias to the netns host (port
# preserved) so the box acts as a real cone-NAT gateway -- inbound and outbound become one
# conntrack flow, no clash. Confirmed live: with this rule the punch promotes to a kcp link
# on BOTH peers; without it both punchers emit and packets reach each peer's lan0, but
# inbound is never delivered to the netns and the punch times out ("context deadline
# exceeded"). This makes the NAT a full-cone (endpoint-independent) NAT -- the permissive
# punchable type; a restricted-cone/symmetric simulation would need a separate router VM.
nft add chain ip nat prerouting '{ type nat hook prerouting priority -100 ; }'
nft add rule ip nat prerouting iif "$lan" ip daddr "$pub" dnat to 192.168.99.2

# move astrald into the netns: join only the NET namespace (mount ns untouched, so the
# apphost unix socket stays reachable from the root ns for astral-query).
mkdir -p /etc/systemd/system/astrald.service.d
cat > /etc/systemd/system/astrald.service.d/netns.conf <<UNIT
[Service]
NetworkNamespacePath=/run/netns/priv
UNIT
systemctl daemon-reload
systemctl restart astrald

# wait for astrald to come back up inside the netns
ok=; n=0
while [ "$n" -lt 90 ]; do
  if systemctl is-active --quiet astrald && timeout 5 ip netns exec priv astral-query localnode:.spec -out json >/dev/null 2>&1; then
    ok=1; break
  fi
  n=$((n + 1)); sleep 1
done
if [ -z "$ok" ]; then
  echo "enter-nat: astrald did not come back up in netns on $(hostname) after ${n}s" >&2
  systemctl status astrald --no-pager >&2 2>&1 || true
  journalctl -u astrald --no-pager 2>&1 | tail -30 >&2 || true
  exit 1
fi

# sanity: astrald must now be in the netns (its own 10.77 endpoint withdrawn) and see 192.168.99.2
in_ns=$(ip netns identify "$(pgrep -x astrald | head -1)" 2>/dev/null || true)
echo "enter-nat: $(hostname) astrald behind NAT (priv 192.168.99.2 -> public $pub via $lan; netns=${in_ns:-?})"
EOS
)

# why: $VMS is a space-separated list -> word-splitting is intentional
# shellcheck disable=SC2086
for vm in $VMS; do
  echo "enter-nat: putting $vm behind its NAT ..."
  # shellcheck disable=SC2029
  netsim ssh "$vm" -- "$REMOTE_BODY"
done
echo "enter-nat: done ($VMS)"
