#!/bin/sh
# bootstrap-user-software-key: turn the operator node into a User node via the Qwen agent in the VM.
#   bootstrap-user-software-key [--vm <host>]      (default: node1 — the VM carrying Qwen)
# Runs on the host; cwd = simulation root.
# why: whole remote program travels as one argv to `netsim ssh`; prompt rides base64-encoded to survive multi-line shell quoting.
set -eu

VM="node1"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm) [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    *)    echo "usage: bootstrap-user-software-key [--vm <host>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is a one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
prompt_b64=$(base64 -w0 "$here/prompt.md")   # note: GNU coreutils -w0 = single line

# shared Qwen dispatch: decode prompt -> qwen -y as tester -> log-tail
. "$(dirname -- "$here")/_lib/agent.sh"

# note: cheap smoke-check only; verify.sh is authoritative. Agent records its output in /home/tester/user.json.
SMOKE=$(cat <<'EOS'
uid=$(python3 -c 'import json;print(json.load(open("/home/tester/user.json")).get("user_id",""))' 2>/dev/null || true)
[ -n "$uid" ] || { echo "agent recorded no user_id in /home/tester/user.json on $(hostname)" >&2; exit 1; }
echo "bootstrap-user-software-key: agent finished on $(hostname); User id $uid"
EOS
)

echo "bootstrap-user-software-key: driving Qwen operator on $VM ..."
# note: assignment prefix carries the prompt to the guest; body re-parses it
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body bootstrap-user-software-key)
$SMOKE"
echo "bootstrap-user-software-key: done on $VM"
