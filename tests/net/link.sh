#!/bin/sh
# link.sh — register every vmop and every remaining netsim task with netsim.
# netsim discovers tasks only in ~/.local/share/netsim/tasks/, so symlink each
# directory holding a run.sh there. Idempotent; re-run anytime.
set -eu

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
tests=$(dirname -- "$here")
dest="${NETSIM_HOME:-$HOME/.local/share/netsim}/tasks"
mkdir -p "$dest"

found=0
# fixtures/vmops/* are the named VM operations a netsim test runs as steps.
# net/tasks/* is pre-unification residue; M4 absorbs it and this glob goes.
for rs in "$tests"/fixtures/vmops/*/run.sh "$tests"/net/tasks/*/run.sh; do
    [ -f "$rs" ] || continue
    d=$(dirname "$rs")
    ln -sfn "$d" "$dest/$(basename "$d")"
    echo "linked $(basename "$d")"
    found=$((found + 1))
done

[ "$found" -gt 0 ] || {
    echo "no run.sh under $tests/fixtures/vmops or $tests/net/tasks" >&2
    exit 1
}
echo "done: $found task(s) registered — run 'netsim tasks' to confirm"
