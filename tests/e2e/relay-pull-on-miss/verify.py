#!/usr/bin/env python3
"""Oracle: node1 routed its first call to the greeter by one pull from node2.

The driver held every step: node1 held no relay contract for the greeter, the
visitor's one call was answered within 1.5 s, and node1 then held the
contract. node1's log shows one ask for the greeter, and the contract pulled
from node2. The oracle repeats the story with fresh identities (`driver
verify`).
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["greeter_serves_on_node2", "node1_held_no_contract", "visitor_greeted",
         "call_within_budget", "node1_holds_the_contract"]

session = load()
facts = session["facts"].get("relay_pull_on_miss", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"
assert facts["call_ms"] < 1500, f"the first call took {facts['call_ms']} ms; want under 1500"

tap = facts["tap"]
assert tap["refused"] is None and tap["broken"] is None, f"node1's log tap is incomplete: {tap}"
starts = [a for a in facts["asks"] if a.startswith("asking linked siblings")]
assert len(starts) == 1, f"node1 asked for the greeter {len(starts)} times; want 1: {facts['asks']}"
node2 = session["nodes"]["node2"]["identity"]
assert any(node2 in p for p in facts["pulled"]), \
    f"node1 logged no relay contract of the greeter pulled from node2: {facts['pulled']}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/relay-pull-on-miss/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print(f"oracle: node1 pulled the greeter's contract from node2 once; "
      f"the first call took {facts['call_ms']} ms")
