#!/usr/bin/env python3
"""Oracle: every step held, and the story holds again on its own.

The oracle repeats the whole story with a fresh app and a fresh repository
(`driver verify`): node1 tells a subscribed indexer app about a note stored
in an indexed repository, and refuses the app's next subscription once its
grant is revoked.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = ["registered_indexer", "told_about_the_note"]

# the one check without the grant: node1 refuses the app's subscription
STEPS += ["revoked_app_cannot_subscribe"]

facts = load()["facts"].get("app_index_provider", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/app-index-provider/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle round trip failed: exit {proc.returncode}"
print("oracle: an indexer app on node1 is told about a note the User stores")
