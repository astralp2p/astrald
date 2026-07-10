#!/bin/sh
# verify configure-astral-agent (same args as run.sh): the astral-agent skill is
# installed for the operator where Qwen Code reads it
# (~<user>/.qwen/skills/astral-agent), with SKILL.md frontmatter intact, the
# references/ dir, and the astral-docs mount present, owned by the operator.
set -eu

VM=node1
USER_NAME=tester
while [ $# -gt 0 ]; do
  case "$1" in
    --vm)   VM=$2; shift 2 ;;
    --user) USER_NAME=$2; shift 2 ;;
    *) shift ;;
  esac
done

REMOTE_CHECK=$(cat <<'EOS'
set -eu
home=$(getent passwd "$u" | cut -d: -f6)
d="$home/.qwen/skills/astral-agent"
[ -f "$d/SKILL.md" ]              || { echo "missing $d/SKILL.md on $(hostname)"          >&2; exit 1; }
head -n1 "$d/SKILL.md" | grep -qx -- '---' || { echo "SKILL.md frontmatter missing on $(hostname)" >&2; exit 1; }
[ -d "$d/references" ]            || { echo "missing references/ on $(hostname)"          >&2; exit 1; }
[ -f "$d/astral-docs/README.md" ] || { echo "astral-docs mount missing on $(hostname)"   >&2; exit 1; }
owner=$(stat -c '%U' "$d")
[ "$owner" = "$u" ] || { echo "astral-agent owned by '$owner', expected '$u' on $(hostname)" >&2; exit 1; }
echo "$(hostname): astral-agent present for $u ($(find -L "$d" -type f 2>/dev/null | wc -l) files via symlinks), frontmatter intact"
EOS
)

netsim ssh "$VM" -- "u='$USER_NAME'; $REMOTE_CHECK" \
  || { echo "configure-astral-agent verify FAILED on $VM" >&2; exit 1; }
echo "verified astral-agent skill on: $VM"
