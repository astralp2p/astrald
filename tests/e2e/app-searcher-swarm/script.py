#!/usr/bin/env python3
"""Driver: a swarm search on node1 reaches an app searcher on node2 once.

The app is a Go program (driver/) served through lib/apps of this
repository's pinned astral-go, the SDK an app author uses. It prints one JSON
line of facts: which steps held.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-searcher-swarm/driver", "drive", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: the recipes app answered the swarm search")
