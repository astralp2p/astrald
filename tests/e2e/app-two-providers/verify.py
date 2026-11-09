#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`):
a consumer on node1 granted discovery of echo-pair finds a provider on node1
and a provider on node2 with reach=swarm, and each provider answers the one
call made to it, and no other.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["consumer_found_both", "node1_knows_both_hosts", "each_provider_echoed",
         "each_provider_answered_its_own_call"]

facts = load()["facts"].get("app_two_providers", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-two-providers/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: an app on node1 finds providers on node1 and node2, and each answers its own call")
