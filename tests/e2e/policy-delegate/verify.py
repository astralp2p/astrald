#!/usr/bin/env python3
"""Oracle: every delegate check held, and the authorizers survived a restart.

The runner restarts node1 between driver and oracle (test.toml `restart`).
The oracle then finds the action-wide and the actor-specific authorizer the
driver set, and removes them one by one (`driver verify`).
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
CHECKS = [
    "hold_delivered_token",
    "disconnect_ended_question",
    "disconnect_provisioned_nothing",
    "setter_refused_11m",
    "timeout_refused_after_5s",
    "decision_authorizer_recorded",
    "decision_authorizer_asked",
    "authorizers_set",
]

session = load()
facts = session["facts"].get("policy_delegate", {})
missing = [c for c in CHECKS if facts.get(c) is not True]
assert not missing, f"checks that did not hold: {missing}"
restarts = session["nodes"]["node1"].get("restarts", 0)
assert restarts >= 1, "node1 was not restarted between driver and oracle"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/policy-delegate/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"authorizers after the restart: exit {proc.returncode}"
print("oracle: both authorizers survived the restart and were removed one by one")
