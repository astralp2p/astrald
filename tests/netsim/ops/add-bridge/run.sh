#!/bin/sh
# add-bridge: give each named VM a container bridge holding an address, as Docker's docker0 does.
#   add-bridge <vm>... [--name <ifname>] [--cidr <address/prefix>]   (default: docker0, 172.17.0.1/16)
#
# why a bridge: a node on a host that runs containers holds the bridge's address among its
#   interface addresses, and the address is reachable from that host alone. astrald drops the
#   addresses of such interfaces from what it advertises (mod/ip/src/advertisable.go); this op
#   gives a test the interface to judge that by.
# why the name is checked: astrald recognises a host-only interface by its name alone, so a
#   name outside its list (mod/ip/src/advertisable.go hostOnlyIfacePrefixes, dockerNetworkIface)
#   makes an ordinary interface the test would misread.
# note: the bridge has no ports, so it carries no traffic; its address is what astrald reads.
# note: idempotent: an existing bridge keeps its name and gets the address replaced.
set -eu

VMS=""; NAME="docker0"; CIDR="172.17.0.1/16"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    --name) [ $# -ge 2 ] || { echo "need an interface name after --name" >&2; exit 64; }; NAME=$2; shift 2 ;;
    --cidr) [ $# -ge 2 ] || { echo "need address/prefix after --cidr" >&2; exit 64; }; CIDR=$2; shift 2 ;;
    *)      echo "usage: add-bridge <vm>... [--name <ifname>] [--cidr <address/prefix>]" >&2; exit 64 ;;
  esac
done
[ -n "$VMS" ] || { echo "add-bridge: name at least one VM" >&2; exit 64; }
case "$NAME" in
  docker*|podman*|cni*|virbr*|veth*|lxcbr*|lxdbr*|vboxnet*|vmnet*) : ;;
  *) echo "$NAME" | grep -Eq '^br-[0-9a-f]{12}$' \
       || { echo "add-bridge: '$NAME' is not a host-only interface name astrald recognises" >&2; exit 64; } ;;
esac
python3 -c 'import ipaddress,sys; ipaddress.ip_interface(sys.argv[1])' "$CIDR" 2>/dev/null \
  || { echo "add-bridge: --cidr takes <address>/<prefix>, not '$CIDR'" >&2; exit 64; }

REMOTE_BODY=$(cat <<'EOS'
set -eu
ip link show dev "$br_name" >/dev/null 2>&1 || ip link add "$br_name" type bridge
ip addr replace "$br_cidr" dev "$br_name"
ip link set "$br_name" up
echo "add-bridge: $(hostname) holds $br_cidr on $br_name"
EOS
)

# shellcheck disable=SC2086
for vm in $VMS; do
  # shellcheck disable=SC2029
  netsim ssh "$vm" -- "br_name='$NAME'; br_cidr='$CIDR'
$REMOTE_BODY"
done
echo "add-bridge: done on: $VMS"
