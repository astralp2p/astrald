#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`): a
player app on node1 and a player app on node2 each serve ping, each pings the
other by its app identity, and each answer names the pinging player as caller.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["player1_serves_on_node1", "player2_serves_on_node2",
         "node1_knows_player2", "node2_knows_player1",
         "player1_pinged_player2", "player2_pinged_player1"]

facts = load()["facts"].get("app_ping_pong", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-ping-pong/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: an app on node1 and an app on node2 ping each other, each seen as itself")
