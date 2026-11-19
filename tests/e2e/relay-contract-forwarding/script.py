#!/usr/bin/env python3
"""Driver: node3 joins through node1 and learns node2's app from node1 alone.

node2 and node3 cannot dial anyone, so no link between them can form. The flow
runs in a Go program (driver/) built from this repository's pinned astral-go.
It prints one JSON line of facts: which steps held.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/relay-contract-forwarding/driver", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: node3 learned node2's app from node1 and called it through node2")
