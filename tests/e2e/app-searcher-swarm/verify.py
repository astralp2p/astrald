#!/usr/bin/env python3
"""Oracle: every step held, and the flow holds again on its own.

The oracle repeats the whole flow with a fresh recipes app and a fresh recipe
(`driver verify`): the User's swarm search on node1 reaches the app on node2
once, and the match names the app as its source.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = [
    "searcher_refused_without_grant",
    "registered_as_searcher",
    "node1_search_found_the_recipe",
    "result_names_the_app_as_source",
    "app_asked_once",
]

facts = load()["facts"].get("app_searcher_swarm", {})
missing = [s for s in STEPS if facts.get(s) is not True]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-searcher-swarm/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle flow failed: exit {proc.returncode}"
print("oracle: a swarm search on node1 is answered once by the recipes app on node2")
