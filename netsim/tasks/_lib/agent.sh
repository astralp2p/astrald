# _lib/agent.sh -- shared Qwen-agent dispatch for the netsim task run.sh scripts.
#
# Source it (host-side), build the remote program from `agent_run_body <task>`,
# and hand it to `netsim ssh` exactly as the scripts always have:
#
#   . "$(dirname -- "$here")/_lib/agent.sh"
#   SMOKE=$(cat <<'EOS'
#   ... per-task soft smoke check ...
#   EOS
#   )
#   netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body <task>)
#   $SMOKE"
#
# `agent_run_body` emits the invariant remote block: decode the base64 prompt the
# caller injects as $prompt_b64 into ~tester/.netsim/<task>.prompt, chown it, run
# `qwen -y` as tester (its stdout/stderr to <task>.log), and on failure tail the
# log and exit 1. Per-task smoke checks and any extra artifacts (e.g.
# object-store's payload.txt) stay in the caller. POSIX sh; no bashisms.

agent_run_body() {
	_agent_task=$1
	printf '%s\n' \
"set -eu" \
"d=/home/tester/.netsim" \
'mkdir -p "$d"' \
"printf '%s' \"\$prompt_b64\" | base64 -d > \"\$d/${_agent_task}.prompt\"" \
'chown -R tester:tester "$d"' \
"su - tester -c 'qwen -y \"\$(cat /home/tester/.netsim/${_agent_task}.prompt)\"' \\" \
"   > \"\$d/${_agent_task}.log\" 2>&1 || {" \
'     echo "qwen run failed on $(hostname); tail of log:" >&2' \
"     tail -n 40 \"\$d/${_agent_task}.log\" >&2" \
'     exit 1' \
'   }'
}
