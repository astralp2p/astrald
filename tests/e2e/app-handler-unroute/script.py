#!/usr/bin/env python3
"""Driver: node1 stops routing to an app's handler once the app's session ends.

astral-py speaks the pre-v1 Services wire, so the flow runs in a Go program
(driver/) built from this repository's pinned astral-go. It re-executes itself
as child processes for the crash legs, and prints one JSON line of facts: which
legs held.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-handler-unroute/driver", "drive", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: node1 stopped routing to each ended app session")
