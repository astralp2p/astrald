#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with a fresh app and a fresh word
(`driver verify`): node1 asks a registered searcher app, relays its result
sourced to the app, and stops asking it once its grant is revoked.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["registered_as_searcher", "search_returned_the_note",
         "source_is_the_app", "app_was_asked_once"]

# the one check without the grant: node1 no longer asks the app
STEPS += ["revoked_app_is_not_asked"]

facts = load()["facts"].get("app_search_provider", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-search-provider/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle round trip failed: exit {proc.returncode}"
print("oracle: the User's objects.search on node1 returns a searcher app's result, sourced to the app")
