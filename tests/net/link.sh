#!/bin/sh
# link.sh — register every task under tasks/ as a netsim user task.
# netsim only discovers tasks in ~/.local/share/netsim/tasks/, so symlink each
# task dir (each folder under tasks/ with a run.sh) there. Idempotent; re-run anytime.
set -eu

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
repo=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
dest="${NETSIM_HOME:-$HOME/.local/share/netsim}/tasks"
mkdir -p "$dest"

found=0
# a "task" = any folder under tasks/ that contains a run.sh
for rs in "$repo"/tasks/*/run.sh; do
    [ -f "$rs" ] || continue
    d=$(dirname "$rs")
    ln -sfn "$d" "$dest/$(basename "$d")"
    echo "linked $(basename "$d")"
    found=$((found + 1))
done

[ "$found" -gt 0 ] || { echo "no tasks (folders with run.sh) found in $repo/tasks" >&2; exit 1; }
echo "done: $found task(s) registered — run 'netsim tasks' to confirm"
