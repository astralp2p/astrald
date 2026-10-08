#!/usr/bin/env python3
"""Oracle: every step held, and the round trip works on its own.

The oracle repeats the whole round trip with fresh identities (`driver verify`):
a provider app registered on node2 after the link is reachable from node1 by
its app identity, with no relink, once apphost.register has pushed its relay
contract.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["discovered_on_node2", "called_without_relink", "provider_saw_the_app"]

facts = load()["facts"].get("services_app_call", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-app-call/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle round trip failed: exit {proc.returncode}"
print("oracle: an app on node2 is called by its identity from node1 with no relink")
