#!/usr/bin/env python3
"""Driver: an app serves objects.describe for node1, and the User's
describe returns the app's descriptor.

The app is a Go program (driver/) built from this repository's pinned
astral-go, whose lib/apps serves objects.describe and keeps the describer
registration's lease. It prints one JSON line of facts: which steps held.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-describe-provider/driver", "drive", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: the app's descriptor reached the User")
