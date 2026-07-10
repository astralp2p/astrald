#!/bin/sh
# object-store: node1 stores an astral object on a chosen target node and reads it back.
# --target is an astral query target: localnode (default, node1's own repo) or an alias (e.g. <node2>:objects.store).
# Aliases (node1/node2) are registered by adopt-node when the swarm forms.
#   object-store [--vm <host>] [--target <localnode|node1|node2|...>]
# Runs on the host. verify.py independently re-reads the object from the holder's local repo.
set -eu

VM="node1"; TARGET="localnode"
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)     [ $# -ge 2 ] || { echo "need host after --vm" >&2; exit 64; }; VM=$2; shift 2 ;;
    --target) [ $# -ge 2 ] || { echo "need an address after --target" >&2; exit 64; }; TARGET=$2; shift 2 ;;
    *)        echo "usage: object-store [--vm <host>] [--target <addr>]" >&2; exit 64 ;;
  esac
done

# CDPATH= is a one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -f "$here/prompt.md" ] || { echo "missing $here/prompt.md" >&2; exit 1; }
# note: aliases are [a-z0-9.], sed-safe
prompt=$(sed "s|__TARGET__|$TARGET|g" "$here/prompt.md")
prompt_b64=$(printf '%s' "$prompt" | base64 -w0)   # note: GNU coreutils -w0 = single line
[ -f "$here/payload.txt" ] || { echo "missing $here/payload.txt" >&2; exit 1; }
payload_b64=$(base64 -w0 "$here/payload.txt")       # note: fixed bytes the agent stores

REMOTE_BODY=$(cat <<'EOS'
set -eu
d=/home/tester/.netsim
mkdir -p "$d"
printf '%s' "$prompt_b64" | base64 -d > "$d/object-store.prompt"
printf '%s' "$payload_b64" | base64 -d > /home/tester/payload.txt
chown -R tester:tester "$d"
chown tester:tester /home/tester/payload.txt

su - tester -c 'qwen -y "$(cat /home/tester/.netsim/object-store.prompt)"' \
   > "$d/object-store.log" 2>&1 || {
     echo "qwen run failed on $(hostname); tail of log:" >&2
     tail -n 40 "$d/object-store.log" >&2
     exit 1
   }

# Cheap smoke-check; verify.py does the authoritative read-back + byte match. The
# agent only stores and records the id in $HOME/object.json (/home/tester/object.json).
oid=$(python3 -c 'import json;print(json.load(open("/home/tester/object.json")).get("object_id",""))' 2>/dev/null || true)
[ -n "$oid" ]  || { echo "agent recorded no object_id in /home/tester/object.json on $(hostname)" >&2; exit 1; }
case "$oid" in
  data1*) : ;;
  *) echo "WARNING $(hostname): object_id does not look like a data1… Object ID (verify.py decides)" >&2 ;;
esac
echo "object-store: agent finished on $(hostname); stored object $oid"
EOS
)

echo "object-store (target=$TARGET): driving Qwen operator on $VM ..."
# shellcheck disable=SC2029
netsim ssh "$VM" -- "prompt_b64='$prompt_b64'; payload_b64='$payload_b64'; $REMOTE_BODY"
echo "object-store (target=$TARGET): done on $VM"
