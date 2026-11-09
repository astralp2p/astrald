#!/usr/bin/env python3
"""Oracle: every step held, and the flow holds again on its own.

The oracle repeats the whole flow with a fresh notes app and a fresh note
(`driver verify`): node2's finders name the app, and reads on node2 and on
node1 come back with bytes only the app holds.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = [
    "finder_refused_without_grant",
    "registered_as_finder",
    "find_names_the_app",
    "node2_read_the_note",
    "node1_read_the_note",
    "app_served_both_reads",
]

facts = load()["facts"].get("app_finder_read", {})
missing = [s for s in STEPS if facts.get(s) is not True]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-finder-read/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle flow failed: exit {proc.returncode}"
print("oracle: reads on node2 and node1 are answered from the notes app")
