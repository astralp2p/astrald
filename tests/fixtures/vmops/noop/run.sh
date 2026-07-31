#!/bin/sh
# noop: boot the world and change nothing.
# why: netsim offers `task` and `story` as its only ways to start a simulation
# from a stage, and both need something to run — an empty story is refused
# outright ("story has no tasks"). The executor needs a boot verb, so the
# harness supplies the smallest possible task.
set -eu
echo "noop: simulation is up; nothing to do"
