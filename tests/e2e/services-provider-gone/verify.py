#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`):
a consumer on node1 follows spg-echo with reach=swarm; a provider process on
node2 is shown, stopped cleanly (removed), restarted (shown again), killed
(removed) and restarted (shown again), and each time it is gone a one-shot
swarm discovery names no provider.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["follow_starts_empty", "follow_shows_the_provider",
         "clean_stop_removes_the_provider", "clean_stop_leaves_no_provider",
         "restart_shows_the_provider_again",
         "crash_removes_the_provider", "crash_leaves_no_provider",
         "restart_after_crash_shows_the_provider", "final_stop_removes_the_provider"]

facts = load()["facts"].get("services_provider_gone", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-provider-gone/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: a provider on node2 is removed from node1's follow when it stops or crashes, and shown again when it restarts")
