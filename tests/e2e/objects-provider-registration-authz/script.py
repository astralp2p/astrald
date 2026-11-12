#!/usr/bin/env python3
"""Driver: who node1 lets register as a finder, describer or searcher.

The apps are a Go program (driver/) built on tests/e2e/internal/provider and
this repository's pinned astral-go. It prints one JSON line of facts: every
register reply, every dial count, and which checks held.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/objects-provider-registration-authz/driver", "drive",
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
print("driver: node1 answered the registration matrix")
