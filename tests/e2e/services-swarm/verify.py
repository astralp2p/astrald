#!/usr/bin/env python3
"""Oracle: every scenario step held, and swarm discovery works on its own.

The facts say which steps the driver saw hold. The oracle does not trust them
alone: it runs one swarm round trip of its own with fresh identities.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = [
    "swarm_refused_beyond_scope", "local_two_service_binding", "swarm_one_shot_complete",
    "member_evaluates_the_app", "swarm_follow_initial", "change_gives_a_new_view",
    "member_provider_loss_removes", "member_provider_returns", "silent_provider_incomplete",
    "unregistered_bare_eos",
]

facts = load()["facts"].get("services_swarm", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-swarm/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle round trip failed: exit {proc.returncode}"
print(proc.stdout.strip())
