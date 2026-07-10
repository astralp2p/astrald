#!/bin/sh
# adopt-node: adopt the second node into the User's swarm, driven by the Qwen
# Code agent running INSIDE node1 (which is already a User node from
# bootstrap-user-software-key — default starting stage: one-node).
#   adopt-node [--vm <host>]      (default: node1 — the VM carrying Qwen)
#
# Runs ON THE HOST (cwd = simulation root). Same mechanic as bootstrap-user-software-key:
# tiny script, thin prompt, intelligence in the agent's astral-agent skill. The
# whole remote program travels as ONE argv to `netsim ssh`; the prompt rides
# along base64-encoded so a multi-line file never fights shell quoting.
set -eu

VM="node1"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    *)    echo "usage: adopt-node [--vm <host>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
prompt_b64=$(base64 -w0 "$here/prompt.md")   # GNU coreutils; -w0 = single line

# shared Qwen dispatch: decode prompt -> qwen -y as tester -> log-tail (_lib/agent.sh)
. "$(dirname -- "$here")/_lib/agent.sh"

# Soft smoke-check only (verify.sh is the authoritative, independent check). node1
# holds the User token in $HOME/user.json, so we can peek at the swarm here; don't
# fail the run on a shape mismatch — leave the verdict to verify.sh.
SMOKE=$(cat <<'EOS'
if [ -n "$(python3 -c 'import json;print(len(json.load(open("/home/tester/siblings.json")).get("sibling_ids") or []))' 2>/dev/null | grep -v '^0$')" ]; then
  echo "adopt-node: $(hostname) recorded swarm siblings in siblings.json"
else
  echo "adopt-node: WARNING $(hostname) recorded no sibling_ids in siblings.json (verify.sh decides)" >&2
fi
ASTRALD_APPHOST_TOKEN=$(python3 -c 'import json;print(json.load(open("/home/tester/user.json")).get("user_token",""))' 2>/dev/null || true)
if [ -n "$ASTRALD_APPHOST_TOKEN" ]; then
  export ASTRALD_APPHOST_TOKEN
  if astral-query user.swarm_status -out json 2>/dev/null | grep -q '"Linked":true'; then
    echo "adopt-node: $(hostname) reports a linked sibling"
  else
    echo "adopt-node: WARNING $(hostname) shows no linked sibling yet (verify.sh decides)" >&2
  fi
fi
echo "adopt-node: agent finished on $(hostname)"
EOS
)

echo "adopt-node: driving Qwen operator on $VM ..."
# assignment prefix carries the prompt to the guest; body re-parses it
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body adopt-node)
$SMOKE"

# Register friendly node aliases (node1/node2) on BOTH nodes so later tasks can
# address nodes by name (object-store --target node2, read of <node1>:..., etc.).
# Host-side; identities resolved from the mutual link (anonymous nodes.links).
# CONFIRM (live): dir.set_alias works for the anonymous host-side caller.
PEER="node2"
_remote_id() {  # $1 = vm; prints the first RemoteIdentity from its nodes.links
  netsim ssh "$1" -- "astral-query nodes.links -out json" 2>/dev/null | python3 -c '
import json,sys
for ln in sys.stdin:
    ln=ln.strip()
    if not ln: continue
    try: o=json.loads(ln)
    except Exception: continue
    ob=o.get("Object")
    if isinstance(ob,dict) and ob.get("RemoteIdentity"):
        print(ob["RemoteIdentity"]); break'
}
node2_id=$(_remote_id "$VM" || true)     # node1's link -> node2
node1_id=$(_remote_id "$PEER" || true)   # node2's link -> node1
if [ -n "$node1_id" ] && [ -n "$node2_id" ]; then
  for vm in "$VM" "$PEER"; do
    netsim ssh "$vm" -- "astral-query dir.set_alias -id '$node1_id' -alias node1 >/dev/null 2>&1; astral-query dir.set_alias -id '$node2_id' -alias node2 >/dev/null 2>&1" || true
  done
  echo "adopt-node: registered aliases node1=$node1_id node2=$node2_id on $VM + $PEER"
else
  echo "adopt-node: WARNING could not resolve node identities for aliases (n1='$node1_id' n2='$node2_id')" >&2
fi
echo "adopt-node: done on $VM"
