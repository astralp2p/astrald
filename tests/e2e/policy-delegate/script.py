#!/usr/bin/env python3
"""Driver: app registration delegated to an app, and authorizer assignments.

astral-py speaks no delegate decision, so the flow runs in a Go program
(driver/) built from this repository's pinned astral-go. It prints one JSON
line of facts: which checks held, and the two authorizers the oracle finds
after the harness restarts the node.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/policy-delegate/driver", "drive", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: delegate checks ran; two authorizers set")
