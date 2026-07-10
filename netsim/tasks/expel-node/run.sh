#!/bin/sh
# expel-node: the User (node1's Qwen operator) permanently bans the peer node from
# the swarm, driven by the Qwen Code agent running INSIDE node1. node1 is already a
# User node with node2 adopted into its swarm (default starting stage: two-nodes).
#   expel-node [--vm <host>]      (default: node1 — the VM carrying Qwen)
#
# Runs ON THE HOST (cwd = simulation root). Same mechanic as adopt-node: tiny script,
# thin prompt, intelligence in the agent's astral-agent skill. The whole remote
# program travels as ONE argv to `netsim ssh`; the prompt rides along base64-encoded
# so a multi-line file never fights shell quoting.
set -eu

VM="node1"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    *)    echo "usage: expel-node [--vm <host>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
prompt_b64=$(base64 -w0 "$here/prompt.md")   # GNU coreutils; -w0 = single line

# shared Qwen dispatch: decode prompt -> qwen -y as tester -> log-tail (_lib/agent.sh)
. "$(dirname -- "$here")/_lib/agent.sh"

# Soft smoke-check only (verify.py is the authoritative, independent check). node1
# holds the User token in $HOME/user.json, so we can peek at the swarm here; don't
# fail the run on a shape mismatch — leave the verdict to verify.py.
SMOKE=$(cat <<'EOS'
ASTRALD_APPHOST_TOKEN=$(python3 -c 'import json;print(json.load(open("/home/tester/user.json")).get("user_token",""))' 2>/dev/null || true)
if [ -n "$ASTRALD_APPHOST_TOKEN" ]; then
  export ASTRALD_APPHOST_TOKEN
  if astral-query user.list_expelled -out json 2>/dev/null | grep -q '"Subject"'; then
    echo "expel-node: $(hostname) records at least one expelled node"
  else
    echo "expel-node: WARNING $(hostname) shows no expelled node yet (verify.py decides)" >&2
  fi
fi
echo "expel-node: agent finished on $(hostname)"
EOS
)

echo "expel-node: driving Qwen operator on $VM ..."
# assignment prefix carries the prompt to the guest; body re-parses it
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body expel-node)
$SMOKE"
echo "expel-node: done on $VM"
