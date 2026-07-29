#!/bin/sh
# expel-node: the User permanently bans the peer node from the swarm, via the Qwen agent in node1.
#   expel-node [--vm <host>]      (default: node1 — the VM carrying Qwen)
# Runs on the host; cwd = simulation root. Starting stage: two-nodes.
# why: whole remote program travels as one argv to `netsim ssh`; prompt rides base64-encoded to survive multi-line shell quoting.
set -eu

VM="node1"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    *)    echo "usage: expel-node [--vm <host>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is a one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
prompt_b64=$(base64 -w0 "$here/prompt.md")   # note: GNU coreutils -w0 = single line

# shared Qwen dispatch: decode prompt -> qwen -y as tester -> log-tail
. "$(dirname -- "$here")/_lib/agent.sh"

# note: soft smoke-check only; verify.py is authoritative. Peek at the swarm via node1's token in /home/tester/user.json.
# why: never fail the run on a shape mismatch — verify.py decides.
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
# note: assignment prefix carries the prompt to the guest; body re-parses it
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body expel-node)
$SMOKE"
echo "expel-node: done on $VM"
