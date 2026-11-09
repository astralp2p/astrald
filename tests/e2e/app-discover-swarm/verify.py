#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`):
a consumer on node1 granted discovery of echo-swarm finds the provider on
node2 with reach=swarm and gets its own message echoed back from node2.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["consumer_found_the_provider", "node1_knows_the_host", "provider_echoed", "provider_saw_the_consumer"]

facts = load()["facts"].get("app_discover_swarm", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-discover-swarm/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: an app on node1 discovers a provider app on node2 and calls it")
