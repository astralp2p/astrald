#!/bin/sh
# link.sh — register this tree's netsim ops with netsim.
# netsim discovers tasks only in ~/.local/share/netsim/tasks/, so symlink each
# directory holding a run.sh there. Idempotent; re-run anytime.
#
# why one glob: a lab recipe's tasks and a test's steps are the same species —
# same file shape, same flat netsim namespace — and netsim cannot tell them
# apart. They live in one directory so a shared basename is impossible rather
# than order-dependent, since `ln -sfn` would silently let the later one win.
set -eu

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
tests=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
dest="${NETSIM_HOME:-$HOME/.local/share/netsim}/tasks"
mkdir -p "$dest"

found=0
for rs in "$tests"/netsim/ops/*/run.sh; do
    [ -f "$rs" ] || continue
    d=$(dirname "$rs")
    ln -sfn "$d" "$dest/$(basename "$d")"
    echo "linked $(basename "$d")"
    found=$((found + 1))
done

[ "$found" -gt 0 ] || {
    echo "no run.sh under $tests/netsim/ops" >&2
    exit 1
}
echo "done: $found task(s) registered — run 'netsim tasks' to confirm"
