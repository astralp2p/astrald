#!/bin/sh
# link.sh — register this tree's netsim tasks with netsim.
# netsim discovers tasks only in ~/.local/share/netsim/tasks/, so symlink each
# directory holding a run.sh there. Idempotent; re-run anytime.
set -eu

# CDPATH= is an intentional one-shot env prefix for cd, not an assignment
# shellcheck disable=SC1007
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
tests=$(dirname -- "$here")   # here is tests/fixtures
dest="${NETSIM_HOME:-$HOME/.local/share/netsim}/tasks"
mkdir -p "$dest"

found=0
# fixtures/vmops/* are the named VM operations a netsim test runs as steps;
# fixtures/lab/tasks/* are the ones lab.story builds the world with.
for rs in "$tests"/fixtures/vmops/*/run.sh "$tests"/fixtures/lab/tasks/*/run.sh; do
    [ -f "$rs" ] || continue
    d=$(dirname "$rs")
    ln -sfn "$d" "$dest/$(basename "$d")"
    echo "linked $(basename "$d")"
    found=$((found + 1))
done

[ "$found" -gt 0 ] || {
    echo "no run.sh under $tests/fixtures/vmops or $tests/fixtures/lab/tasks" >&2
    exit 1
}
echo "done: $found task(s) registered — run 'netsim tasks' to confirm"
