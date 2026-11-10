#!/usr/bin/env python3
"""Oracle: each app got exactly its own calls, and an unserved identity is refused.

The driver's facts must all hold. The oracle then repeats the whole story with
fresh identities (`driver verify`): two apps on node1 serve the same op, a
visitor calls each by identity in an interleaved order and is answered by the
app it addressed, each app records only calls addressed to it, and calls to an
app that never served and to an unknown identity fail with route_not_found
without reaching either app.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["both_serve", "answers_by_target", "only_own_calls", "idle_refused",
         "unknown_refused", "refusals_reach_nobody"]

facts = load()["facts"].get("app_routing_two_apps", {})
assert not facts.get("problem"), f"routing broke: {facts['problem']}"
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-routing-two-apps/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}: {proc.stderr.strip()[-400:]}"
print("oracle: two apps on node1 each receive exactly the queries addressed to them; an unserved identity is refused")
