#!/bin/sh
# link-over-tor: have node1's Qwen agent re-establish the swarm link to the peer
# (node2) over Tor after node2 left the LAN, and confirm the link rides over Tor.
# Driven by the agent following the astral-agent skill's linking-over-tor playbook.
#   link-over-tor [--vm <host>] [--peer <alias>]    (default: node1, node2)
#
# Runs ON THE HOST. Tiny script, thin prompt, intelligence in the skill. verify.py
# then INDEPENDENTLY confirms node1 holds a tor link to the peer.
set -eu

VM="node1"; PEER="node2"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --peer) [ $# -ge 2 ] || { echo "need alias after --peer" >&2; exit 64; }; PEER=$2; shift 2 ;;
    *)      echo "usage: link-over-tor [--vm <host>] [--peer <alias>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
prompt=$(sed "s|__PEER__|$PEER|g" "$here/prompt.md")   # alias is [a-z0-9] — sed-safe
prompt_b64=$(printf '%s' "$prompt" | base64 -w0)

# shared Qwen dispatch: decode prompt -> qwen -y as tester -> log-tail (_lib/agent.sh)
. "$(dirname -- "$here")/_lib/agent.sh"

# Cheap smoke-check; verify.py does the authoritative, independent check. The agent
# records what it read in $HOME/tor.json under link_network (and peer_onion).
SMOKE=$(cat <<'EOS'
net=$(python3 -c 'import json;print(json.load(open("/home/tester/tor.json")).get("link_network",""))' 2>/dev/null || true)
[ -n "$net" ] || { echo "agent recorded no link_network in /home/tester/tor.json on $(hostname)" >&2; exit 1; }
echo "link-over-tor: agent finished on $(hostname); recorded link_network=$net"
EOS
)

echo "link-over-tor: driving Qwen operator on $VM to link with $PEER over Tor ..."
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body link-over-tor)
$SMOKE"
echo "link-over-tor: done on $VM"
