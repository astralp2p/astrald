#!/bin/sh
# bootstrap-user-software-key: turn the operator node into a User-controlled node, driven by
# the Qwen Code agent running INSIDE the VM.
#   bootstrap-user-software-key [--vm <host>]      (default: node1 — the VM carrying Qwen)
#
# Runs ON THE HOST (cwd = simulation root). This script is deliberately tiny: it
# ships prompt.md to the agent on the guest and lets the agent do the astral
# work via astral-query against the local node API. The intelligence lives in
# the prompt and — by design — in the agent's astral-agent skill, not here. The
# whole remote program travels as ONE argv to `netsim ssh` (no reliance on stdin
# forwarding); the prompt rides along base64-encoded so a multi-line file never
# fights shell quoting.
set -eu

VM="node1"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    *)    echo "usage: bootstrap-user-software-key [--vm <host>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
prompt_b64=$(base64 -w0 "$here/prompt.md")   # GNU coreutils; -w0 = single line

# shared Qwen dispatch: decode prompt -> qwen -y as tester -> log-tail (_lib/agent.sh)
. "$(dirname -- "$here")/_lib/agent.sh"

# Cheap smoke-check; verify.sh does the authoritative, independent check. The agent
# records its outputs in $HOME/user.json (/home/tester/user.json).
SMOKE=$(cat <<'EOS'
uid=$(python3 -c 'import json;print(json.load(open("/home/tester/user.json")).get("user_id",""))' 2>/dev/null || true)
[ -n "$uid" ] || { echo "agent recorded no user_id in /home/tester/user.json on $(hostname)" >&2; exit 1; }
echo "bootstrap-user-software-key: agent finished on $(hostname); User id $uid"
EOS
)

echo "bootstrap-user-software-key: driving Qwen operator on $VM ..."
# assignment prefix carries the prompt to the guest; body re-parses it
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body bootstrap-user-software-key)
$SMOKE"
echo "bootstrap-user-software-key: done on $VM"
