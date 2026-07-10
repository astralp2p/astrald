#!/bin/sh
# read-remote-object: node1's agent reads over astral an object that lives on the peer.
# Object id is in node1's /home/tester/object.json, written by object-store --target node2.
# Agent addresses the peer by its alias (registered by adopt-node).
#   read-remote-object [--vm <host>] [--peer <alias>]   (default: node1, node2)
# Runs on the host. verify.py independently re-reads the peer's object as the User and asserts.
# why: read is issued as the User (authenticated) — an anonymous read would not route to the peer.
set -eu

VM="node1"; PEER="node2"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --peer) [ $# -ge 2 ] || { echo "need alias after --peer" >&2; exit 64; }; PEER=$2; shift 2 ;;
    *)      echo "usage: read-remote-object [--vm <host>] [--peer <alias>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is a one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
prompt=$(sed "s|__PEER__|$PEER|g" "$here/prompt.md")   # note: alias is [a-z0-9], sed-safe
prompt_b64=$(printf '%s' "$prompt" | base64 -w0)

# shared Qwen dispatch: decode prompt -> qwen -y as tester -> log-tail
. "$(dirname -- "$here")/_lib/agent.sh"

# note: cheap smoke-check only; verify.py is authoritative. Agent records object_remote in /home/tester/read.json.
SMOKE=$(cat <<'EOS'
rem=$(python3 -c 'import json;print(json.load(open("/home/tester/read.json")).get("object_remote",""))' 2>/dev/null || true)
[ -n "$rem" ] || { echo "agent recorded no object_remote in /home/tester/read.json on $(hostname)" >&2; exit 1; }
echo "read-remote-object: agent finished on $(hostname); read back from peer"
EOS
)

echo "read-remote-object: driving Qwen operator on $VM to read from $PEER ..."
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body read-remote-object)
$SMOKE"
echo "read-remote-object: done on $VM"
