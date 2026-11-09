#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`):
a consumer granted discovery of echo-local finds the provider on node1 and
gets its own message echoed back, and an app without the grant is refused.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["consumer_found_the_provider", "provider_echoed", "provider_saw_the_consumer", "stranger_refused"]

facts = load()["facts"].get("app_discover_local", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-discover-local/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: an app on node1 discovers a provider app on node1 and calls it")
