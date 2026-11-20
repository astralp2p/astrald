#!/bin/sh
# move-subnet: move <vm> to a new subnet --in seconds from now, while the driver watches; with two
#   --to, move it twice, --gap seconds apart.
#   move-subnet [--vm <host>] --to <a.b.c> [--to <a.b.c>]... [--in <seconds>] [--gap <seconds>]
#                                                      (default: node1, --in 30, --gap 3.5)
#
# why scheduled in the guest: a vmop runs before the driver and ends, and the driver has to be
#   watching when the address changes. A transient systemd timer on <vm> runs the moves, as
#   lan-outage schedules a return.
# why a move is add-then-delete: astrald polls net.InterfaceAddrs() every 3 s (mod/ip), so the
#   address set it sees is what changes; and the kernel flushes every route over a NIC that loses
#   its last IPv4 address, so the new address goes on before the old one comes off.
# why on-link routes: the lab is one flat 10.77 LAN segment with no router. Every other VM routes
#   each new subnet over its LAN NIC, and <vm> routes the LAN (10.77) and the NAT aliases
#   (198.51.100, enter-nat and go-public) over its own, so each side still reaches the other.
# note: <vm> keeps its last octet in every subnet: <a.b.c>.<octet of its 10.77 address>.
# note: /run/e2e-move-subnet.env on <vm> holds its LAN NIC, its LAN subnet, its octet and the
#   address it holds now, so a later call moves it on from where the last one left it.
# note: no link is closed here; the driver closes the links that rode an address it saw go.
set -eu

VM="node1"; TO=""; IN=30; GAP=3.5
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)  [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --to)  [ $# -ge 2 ] || { echo "need a.b.c after --to" >&2; exit 64; }; TO="${TO:+$TO }$2"; shift 2 ;;
    --in)  [ $# -ge 2 ] || { echo "need seconds after --in" >&2; exit 64; }; IN=$2; shift 2 ;;
    --gap) [ $# -ge 2 ] || { echo "need seconds after --gap" >&2; exit 64; }; GAP=$2; shift 2 ;;
    *)     echo "usage: move-subnet [--vm <host>] --to <a.b.c>... [--in <seconds>] [--gap <seconds>]" >&2; exit 64 ;;
  esac
done
[ -n "$TO" ] || { echo "move-subnet: name a subnet with --to" >&2; exit 64; }
for p in $TO; do
  echo "$p" | grep -Eq '^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$' \
    || { echo "move-subnet: --to takes the first three octets of a /24, not '$p'" >&2; exit 64; }
done
case "$IN" in ''|*[!0-9]*) echo "move-subnet: --in takes whole seconds, not '$IN'" >&2; exit 64 ;; esac
echo "$GAP" | grep -Eq '^[0-9]+(\.[0-9]+)?$' \
  || { echo "move-subnet: --gap takes seconds, not '$GAP'" >&2; exit 64; }

# 1) on <vm>: find where it stands, write the moves, and schedule them
MOVER_BODY=$(cat <<'EOS'
set -eu
state=/run/e2e-move-subnet.env
if [ ! -f "$state" ]; then
  lan_if=$(ip -o -4 addr show | awk '$4 ~ /^10\.77\./ {print $2; exit}')
  [ -n "$lan_if" ] || { echo "move-subnet: no 10.77 LAN interface on $(hostname)" >&2; exit 1; }
  cidr=$(ip -o -4 addr show dev "$lan_if" | awk '$4 ~ /^10\.77\./ {print $4; exit}')
  home_net=$(python3 -c 'import ipaddress,sys; print(ipaddress.ip_interface(sys.argv[1]).network)' "$cidr")
  oct=${cidr%/*}; oct=${oct##*.}
  printf 'lan_if=%s\nhome_net=%s\noct=%s\ncidr=%s\n' "$lan_if" "$home_net" "$oct" "$cidr" > "$state"
fi
. "$state"
ip -o -4 addr show dev "$lan_if" | awk -v c="$cidr" '$4 == c {f=1} END {exit !f}' \
  || { echo "move-subnet: $(hostname) does not hold $cidr on $lan_if, as $state says" >&2; exit 1; }
for p in $moves; do
  [ "$p.$oct/24" != "$cidr" ] || { echo "move-subnet: $(hostname) already holds $cidr" >&2; exit 1; }
done

script=/run/e2e-move-subnet.sh
cat > "$script" <<'MOVE'
set -eu
state=/run/e2e-move-subnet.env
. "$state"
move() {
  new="$1.$oct/24"
  ip addr add "$new" dev "$lan_if"
  ip addr del "$cidr" dev "$lan_if"
  ip route replace "$home_net" dev "$lan_if"
  ip route replace 198.51.100.0/24 dev "$lan_if"
  echo "move-subnet: $(hostname) moved from $cidr to $new at $(date -u +%FT%T.%NZ)" >&2
  cidr="$new"
  printf 'lan_if=%s\nhome_net=%s\noct=%s\ncidr=%s\n' "$lan_if" "$home_net" "$oct" "$cidr" > "$state"
}
MOVE
first=1
for p in $moves; do
  [ "$first" = 1 ] || echo "sleep $gap" >> "$script"
  echo "move $p" >> "$script"
  first=0
done

systemctl stop e2e-move-subnet.timer 2>/dev/null || true
systemctl reset-failed e2e-move-subnet.service e2e-move-subnet.timer 2>/dev/null || true
systemd-run --quiet --unit=e2e-move-subnet --on-active="${in_s}s" /bin/sh "$script"
echo "move-subnet: $(hostname) holds $cidr on $lan_if; moves to $moves in ${in_s}s, ${gap}s apart" >&2
EOS
)
echo "move-subnet: scheduling $VM's move to $TO in ${IN}s ..."
# shellcheck disable=SC2029
netsim ssh "$VM" -- "moves='$TO'; in_s='$IN'; gap='$GAP'
$MOVER_BODY"

# 2) on every other running VM: route each new subnet over its LAN NIC
ROUTE_BODY=$(cat <<'EOS'
set -eu
lan_if=""
[ -f /run/e2e-move-subnet.env ] && . /run/e2e-move-subnet.env
[ -n "$lan_if" ] || lan_if=$(ip -o -4 addr show | awk '$4 ~ /^10\.77\./ {print $2; exit}')
[ -n "$lan_if" ] || { echo "move-subnet: no LAN interface on $(hostname)" >&2; exit 1; }
for p in $moves; do
  ip route replace "$p.0/24" dev "$lan_if"
done
echo "move-subnet: $(hostname) routes $moves over $lan_if"
EOS
)
VMS=$(netsim vm ls --json | python3 -c \
  'import json,sys; print(" ".join(v["hostname"] for v in json.load(sys.stdin) if v["state"]=="running"))')
for host in $VMS; do
  [ "$host" != "$VM" ] || continue
  # shellcheck disable=SC2029
  netsim ssh "$host" -- "moves='$TO'
$ROUTE_BODY"
done
echo "move-subnet: done; $VM moves to $TO in ${IN}s"
