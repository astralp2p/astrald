#!/bin/sh
# go-public: give a node a public address on the LAN and restart its astrald, so it is the
#   public peer of a punch with a NAT'd node.
#   go-public [--vm <host>]...      (default: node2)
#
# why the alias: a NAT'd node reaches the LAN only from its public alias 198.51.100.<oct>
#   (enter-nat), so its public peer needs an address on that subnet to answer from, and astrald
#   needs a public IPv4 among its own addresses to arm its nat module.
# why the restart: the nat module evaluates its enabled state at start and on an observed
#   endpoint, not when an interface address appears.
# note: the node stays in the root netns; its 10.77 LAN address stays as well.
set -eu

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    *)    echo "usage: go-public [--vm <host>]..." >&2; exit 64 ;;
  esac
done
[ -n "$VMS" ] || VMS="node2"

REMOTE_BODY=$(cat <<'EOS'
set -eu
lan=$(ip -o -4 addr show | awk '$4 ~ /^10\.77\./ {print $2; exit}')
[ -n "$lan" ] || { echo "go-public: no 10.77 LAN interface on $(hostname)" >&2; exit 1; }
oct=$(ip -o -4 addr show dev "$lan" | awk '$4 ~ /^10\.77\./ {n=$4; sub(/\/.*/,"",n); split(n,a,"."); print a[4]; exit}')
pub="198.51.100.$oct"
ip addr add "$pub/24" dev "$lan" 2>/dev/null || true

systemctl restart astrald
ok=; n=0
while [ "$n" -lt 90 ]; do
  if systemctl is-active --quiet astrald && timeout 5 astral-query localnode:.spec -out json >/dev/null 2>&1; then
    ok=1; break
  fi
  n=$((n + 1)); sleep 1
done
[ -n "$ok" ] || { echo "go-public: astrald did not come back up on $(hostname) after ${n}s" >&2; exit 1; }
echo "go-public: $(hostname) public at $pub on $lan"
EOS
)

# shellcheck disable=SC2086
for vm in $VMS; do
  echo "go-public: giving $vm a public address ..."
  netsim ssh "$vm" -- "$REMOTE_BODY"
done
echo "go-public: done ($VMS)"
