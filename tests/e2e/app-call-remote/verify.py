#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`): a
greeter app serves hello on node2, and a visitor app on node1 calls it by the
greeter's app identity and is greeted as itself.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["greeter_serves_on_node2", "node1_knows_the_greeter",
         "visitor_greeted_across_nodes"]

facts = load()["facts"].get("app_call_remote", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-call-remote/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: an app on node1 calls an app on node2 and is seen as itself")
