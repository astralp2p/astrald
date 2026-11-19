#!/usr/bin/env python3
"""Driver: a new link to node2 replaces a black-holed one, and node1 syncs.

The flow runs in a Go program (driver/) built from this repository's pinned
astral-go, which serves the TCP relay the black-holed link runs through. It
prints one JSON line of facts: which steps held, and how long each took.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/reconnect-sync-after-dead-link/driver", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: node1 dropped the black-holed link and node2 learned node1's app")
