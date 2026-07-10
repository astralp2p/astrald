# _lib/agent.sh -- shared Qwen-agent dispatch for the netsim run.sh scripts (POSIX sh).
#
# agent_run_body <task> emits the invariant remote block: decode the base64 prompt
# the caller injects as $prompt_b64 into ~tester/.netsim/<task>.prompt, chown it, run
# `qwen -y` as tester (stdout/stderr -> <task>.log), tail the log + exit 1 on failure.
# Per-task smoke checks and extra artifacts stay in the caller. Usage:
#   . "$(dirname -- "$here")/_lib/agent.sh"
#   netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; $(agent_run_body <task>)
#   $SMOKE"

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
