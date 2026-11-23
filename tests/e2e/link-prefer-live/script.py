#!/usr/bin/env python3
"""Driver: two links to one peer, one of them dark; node1 keeps querying.

The flow runs in a Go program (driver/), which serves two TCP relays on the
test host: phase A darkens the older of two links (relay R1, beside a direct
link D), phase B the newest (relay R2, beside D). It prints one JSON line,
the `prefer_live` fact: the direct link's id, and per phase the dark link's
id, its black-hole time, the time of the last byte its relay forwarded, when
node1 and node2 stopped listing it, and every query node1 sent to node2
(start, end, outcome). The driver judges nothing.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import write_facts

ROOT = Path(__file__).resolve().parents[3]

proc = subprocess.run(
    ["go", "run", "./tests/e2e/link-prefer-live/driver", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")})
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: two phases of a dark link beside a live one ran")
