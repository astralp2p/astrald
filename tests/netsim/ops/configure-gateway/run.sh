#!/bin/sh
# configure-gateway: make one VM a gateway and point others at it.
#   configure-gateway <client-vm>... --gateway <host> [--peer <host>]...
#
# Bare words are the harness's --vm clients: they register with the gateway.
# --gateway names the gateway VM. --peer names a node that only needs to be
# able to reach the gateway, without registering with it.
#
# why an op rather than driver calls: both halves are config, not ops. A node
#   becomes a gateway only with gateway.enabled true (mod/gateway/src/module.go
#   canGateway), and a node registers with one only by listing it under
#   `gateways` — the module then schedules MaintainGatewayConnectionsTask and
#   registers itself (module.go addPersistentGateway). Neither is reachable
#   over apphost, so both need a file and a restart.
# why an explicit endpoint: getGatewayEndpoint falls back to
#   IP.PublicIPCandidates(), and a 10.77 lab address is not public — the
#   gateway would answer "no public IP available". networks.tcp.endpoint is
#   the documented override and is what deps.go parses into configEndpoints.
# why ASTRAL_ID_<vm>: the harness learns every identity at tunnel time and is
#   the authority on it. A vmop that hunts for one by alias works under the
#   script driver and dies under the agent one — the lesson leave-lan already
#   paid for.
# note: astrald's module yaml lives under <root>/config, not <root> —
#   cmd/astrald/run.go joins "config" onto -root. A file one level up is read
#   by nothing and the module silently keeps its defaults, which is a gateway
#   that never gateways and a client that never registers.
# note: run AFTER the roster is up, so every identity is exported.
set -eu

GW=""; CLIENTS=""; PEERS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --gateway) [ $# -ge 2 ] || { echo "need host after --gateway" >&2; exit 64; }; GW=$2; shift 2 ;;
    --vm)      [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; CLIENTS="${CLIENTS:+$CLIENTS }$2"; shift 2 ;;
    --peer)    [ $# -ge 2 ] || { echo "need host after --peer" >&2; exit 64; }; PEERS="${PEERS:+$PEERS }$2"; shift 2 ;;
    *) echo "usage: configure-gateway <client-vm>... --gateway <host>" >&2; exit 64 ;;
  esac
done
[ -n "$GW" ] || { echo "configure-gateway: --gateway is required" >&2; exit 64; }
[ -n "$CLIENTS" ] || { echo "configure-gateway: name at least one client VM" >&2; exit 64; }

eval "GW_ID=\${ASTRAL_ID_$GW:-}"
[ -n "$GW_ID" ] || { echo "configure-gateway: ASTRAL_ID_$GW is not set — is $GW on the manifest roster?" >&2; exit 1; }

GW_ADDR=$(netsim ssh "$GW" -- "ip -o -4 addr show | awk '\$4 ~ /^10\.77\./ {a=\$4; sub(/\/.*/,\"\",a); print a; exit}'")
[ -n "$GW_ADDR" ] || { echo "configure-gateway: no 10.77 address on $GW" >&2; exit 1; }

echo "configure-gateway: $GW is the gateway at $GW_ADDR:1791 ($(echo "$GW_ID" | cut -c1-16)…)"

# 0) give the gateway an address on the NAT peers' network. enter-nat SNATs a
# NAT'd node's traffic to 198.51.100.<oct>, so the gateway receives a SYN from
# a subnet it otherwise has no route back to and the reply goes nowhere — the
# dial hangs for the full query timeout rather than failing. add-reflector
# solves the same problem the same way for nat-punch. Verified: without this
# alias a netns-side `nc` to the gateway hangs; with it, it connects.
netsim ssh "$GW" -- "set -eu
  lan=\$(ip -o -4 addr show | awk '\$4 ~ /^10\.77\./ {print \$2; exit}')
  [ -n \"\$lan\" ] || { echo 'configure-gateway: no 10.77 nic' >&2; exit 1; }
  oct=\$(ip -o -4 addr show dev \$lan | awk '\$4 ~ /^10\.77\./ {n=\$4; sub(/\/.*/,\"\",n); split(n,a,\".\"); print a[4]; exit}')
  ip addr add \"198.51.100.\$oct/24\" dev \$lan 2>/dev/null || true"
echo "configure-gateway: $GW also aliased onto 198.51.100/24 for NAT'd peers"

# 1) the gateway itself: enabled, with an explicit tcp endpoint on the lab LAN.
netsim ssh "$GW" -- "set -eu
mkdir -p /var/lib/astrald/config
cat > /var/lib/astrald/config/gateway.yaml <<YAML
gateway:
  enabled: true
  networks:
    tcp:
      port: 1791
      endpoint: \"$GW_ADDR:1791\"
YAML
systemctl restart astrald"

# 2) each client: name the gateway, be publicly visible in its list.
# why: $CLIENTS is a space-separated list -> word-splitting is intentional
# shellcheck disable=SC2086
for vm in $CLIENTS; do
  netsim ssh "$vm" -- "set -eu
mkdir -p /var/lib/astrald/config
cat > /var/lib/astrald/config/gateway.yaml <<YAML
visibility: public
gateways:
  - $GW_ID
YAML
systemctl restart astrald"
  echo "configure-gateway: $vm now registers with $GW"
done

# 3) wait for astrald to be back on every touched VM before handing over.
# why the netns dance: enter-nat moves astrald into netns priv, and apphost
# then listens on that netns's loopback — a root-ns astral-query reaches
# nothing. Ask where astrald actually is rather than assuming.
for vm in $GW $CLIENTS; do
  ok=
  for _ in $(seq 1 60); do
    if netsim ssh "$vm" -- "set -eu
        q=astral-query
        ip netns list 2>/dev/null | grep -qw priv && q='ip netns exec priv astral-query'
        systemctl is-active --quiet astrald && \$q apphost.whoami >/dev/null 2>&1"; then ok=1; break; fi
    sleep 2
  done
  [ -n "$ok" ] || { echo "configure-gateway: astrald did not come back on $vm" >&2
    netsim ssh "$vm" -- "journalctl -u astrald --no-pager | tail -20" >&2 || true; exit 1; }
done

# 4) everyone who has to reach the gateway needs to know where it lives. A
# lab node knows its swarm siblings from adoption and knows nothing else, so
# without this a client's registration and a peer's relay dial both die as
# `route not found` — measured, before this step existed.
# shellcheck disable=SC2086
for vm in $CLIENTS $PEERS; do
  netsim ssh "$vm" -- "set -eu
    q=astral-query
    ip netns list 2>/dev/null | grep -qw priv && q='ip netns exec priv astral-query'
    \$q nodes.add_endpoint -id $GW_ID -endpoint tcp:$GW_ADDR:1791 >/dev/null"
  echo "configure-gateway: $vm can reach $GW at $GW_ADDR:1791"
done

echo "configure-gateway: done (gateway=$GW clients=$CLIENTS peers=${PEERS:-none})"
