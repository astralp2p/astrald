#!/bin/sh
# read-remote-object: have node1's agent read an astral object that lives on the
# peer (node2), over astral. The object's id is in node1's ~/object.json (object_id,
# written by object-store --target node2). Driven by the Qwen Code agent on node1 —
# the read is issued AS THE USER (authenticated), which routes to the peer (an
# anonymous read would not). The agent addresses the peer by its alias (registered
# by adopt-node).
#   read-remote-object [--vm <host>] [--peer <alias>]   (default: node1, node2)
#
# Runs ON THE HOST. Tiny script, thin prompt, intelligence in the astral-agent skill.
# verify.py then INDEPENDENTLY re-reads the peer's object as the User and asserts.
set -eu

VM="node1"; PEER="node2"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --peer) [ $# -ge 2 ] || { echo "need alias after --peer" >&2; exit 64; }; PEER=$2; shift 2 ;;
    *)      echo "usage: read-remote-object [--vm <host>] [--peer <alias>]" >&2; exit 64 ;;
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
# records what it read in $HOME/read.json under object_remote.
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
