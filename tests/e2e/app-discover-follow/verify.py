#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`):
a consumer on node1 follows echo-follow with reach=swarm, the follow shows a
provider that starts on node2 after it, the consumer gets its own message
echoed back from node2, and the follow removes the provider once it stops.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["follow_starts_empty", "follow_shows_the_provider", "node1_knows_the_host",
         "provider_echoed", "provider_saw_the_consumer", "follow_removes_the_provider"]

facts = load()["facts"].get("app_discover_follow", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-discover-follow/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: an app on node1 follows a service, calls the provider that appears on node2, and sees it removed")
