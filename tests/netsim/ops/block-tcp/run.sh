#!/bin/sh
# block-tcp: drop every tcp packet between the named VMs on the 10.77 LAN, and close the tcp
#   links between them.
#   block-tcp <vm> <vm>...          (bare words are the harness's --vm; at least two)
#
# why tcp only: the pair keeps every other path, kcp over UDP among them, so a test can show
#   that a node reaches its sibling over kcp alone.
# why drop and not reject: a dropped SYN is what a filtered network does; the tcp dial then
#   waits instead of failing, as in the field report that motivated this op.
# why the LAN addresses only: SSH and the harness's apphost tunnel ride the WAN NIC, so the
#   harness keeps reaching every VM.
# why links are closed: an established tcp link has no keepalive that would see the drop
#   soon, and both ends would go on listing a link nothing carries.
# note: the rules live in their own table, e2e_block_tcp, and do not survive a reboot.
set -eu

VMS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VMS="${VMS:+$VMS }$2"; shift 2 ;;
    *)    echo "usage: block-tcp <vm> <vm>..." >&2; exit 64 ;;
  esac
done
# shellcheck disable=SC2086
set -- $VMS
[ $# -ge 2 ] || { echo "block-tcp: name at least two VMs" >&2; exit 64; }

lan_ip() {
  netsim ssh "$1" -- "ip -o -4 addr show | awk '\$4 ~ /^10\.77\./ {a=\$4; sub(/\/.*/,\"\",a); print a; exit}'" | tr -d '\r\n'
}

ADDRS=""
for vm in $VMS; do
  ip=$(lan_ip "$vm")
  case "$ip" in 10.77.*) : ;; *) echo "block-tcp: $vm has no 10.77 address ('$ip')" >&2; exit 1 ;; esac
  ADDRS="${ADDRS:+$ADDRS, }$ip"
done

# 1) drop tcp to and from every named address, on every named VM
for vm in $VMS; do
  netsim ssh "$vm" -- "set -eu
command -v nft >/dev/null 2>&1 || {
  export DEBIAN_FRONTEND=noninteractive
  apt-get -qq -y -o DPkg::Lock::Timeout=120 install nftables >/dev/null
}
nft delete table inet e2e_block_tcp 2>/dev/null || true
nft -f - <<NFT
table inet e2e_block_tcp {
  set peers { type ipv4_addr; elements = { $ADDRS } }
  chain input  { type filter hook input  priority -10; ip saddr @peers meta l4proto tcp drop; }
  chain output { type filter hook output priority -10; ip daddr @peers meta l4proto tcp drop; }
}
NFT"
  echo "block-tcp: $vm drops tcp to and from $ADDRS"
done

# 2) close every tcp link one named VM holds to another
CLOSE_BODY=$(cat <<'EOS'
set -eu
astral-query nodes.links -out json 2>/dev/null | python3 -c "
import json, os, subprocess, sys
peers = set(os.environ['peer_ids'].split())
for line in sys.stdin:
    try:
        m = json.loads(line).get('Object') or {}
    except Exception:
        continue
    if not isinstance(m, dict) or m.get('Network') != 'tcp' or m.get('RemoteIdentity') not in peers:
        continue
    subprocess.run(['astral-query', 'nodes.close_link', '-link_id', str(m['ID'])],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    print('closed tcp link %s' % m['ID'])
"
EOS
)
IDS=""
for vm in $VMS; do
  eval "id=\${ASTRAL_ID_$vm:-}"
  [ -n "$id" ] || { echo "block-tcp: ASTRAL_ID_$vm is not set — is $vm on the manifest roster?" >&2; exit 1; }
  IDS="${IDS:+$IDS }$id"
done
for vm in $VMS; do
  # shellcheck disable=SC2029
  out=$(netsim ssh "$vm" -- "export peer_ids='$IDS'; $CLOSE_BODY" 2>/dev/null || true)
  [ -n "$out" ] && echo "$out" | sed "s/^/block-tcp: $vm /"
done
echo "block-tcp: tcp blocked among $VMS"
