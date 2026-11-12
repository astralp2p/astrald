#!/usr/bin/env python3
"""Driver: finder revoke and re-grant, and finder and searcher leases that lapse.

The apps are a Go program (driver/) built on tests/e2e/internal/provider and
this repository's pinned astral-go. It prints one JSON line of facts: what
each find and search answered, each app's counters, and each register reply.
It judges nothing and stops only on a wiring fault.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/objects-provider-revoke-lease/driver", "drive",
     os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: node1 ran the revoke and lease flow")
