#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with fresh identities (`driver verify`): a
provider app on node1 serves five ops, and a caller app on node1 sends it
unusual typed arguments and streams in both directions, all of which arrive
intact.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = [
    "provider_serves",
    "args_echoed",
    "stream_to_caller",
    "stream_both_ways",
    "bulk_to_caller",
    "bulk_from_caller",
]

facts = load()["facts"].get("app_op_surface", {})
missing = [s for s in STEPS if not facts.get(s)]
failures = facts.get("failures", {})
assert not missing, "steps that did not hold: " + "; ".join(
    f"{s}: {failures.get(s, 'not run')}" for s in missing
)

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-op-surface/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}: {proc.stderr.strip()[-400:]}"
print("oracle: an app on node1 received a query's arguments intact, and its streams ran both ways intact")
