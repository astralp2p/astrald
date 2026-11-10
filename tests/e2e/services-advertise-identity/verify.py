#!/usr/bin/env python3
"""Oracle: every step held, and the identity rules hold again on their own.

The facts say which steps the driver saw hold. The oracle does not trust them
alone: it tells the whole story again with fresh identities (`driver verify`):
a service advertised on node2 is found from node1 under the app's identity,
and an answer naming another provider or service, an advertisement without
ServeApps or over a link, and a taken name each show nothing.
"""
import os
import subprocess
import sys
from pathlib import Path

from lib.sessionio import load

ROOT = Path(__file__).resolve().parents[3]
STEPS = [
    "swarm_shows_the_app", "provider_is_not_a_node", "home_node_shows_the_app",
    "other_node_has_no_local_provider", "provider_evaluated_the_consumer",
    "honest_raw_binding_is_accepted",
    "answer_as_another_provider_shows_nothing", "answer_as_another_provider_ends_the_binding",
    "answer_as_another_provider_shows_nothing_at_home",
    "answer_for_another_service_shows_nothing", "answer_for_another_service_ends_the_binding",
    "no_serve_apps_is_refused", "no_serve_apps_shows_nothing",
    "advertise_over_a_link_is_refused", "advertise_over_a_link_hits_the_origin_guard", "advertise_over_a_link_shows_nothing",
    "taken_name_is_refused", "taken_name_refuses_the_whole_set", "refused_set_shows_nothing",
    "same_name_from_another_app_is_another_provider", "home_node_does_not_show_the_other_nodes_provider",
    "closed_bindings_leave_discovery",
]

facts = load()["facts"].get("services_advertise_identity", {})
missing = [s for s in STEPS if not facts.get(s)]
assert not missing, f"steps that did not hold: {missing}"

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-advertise-identity/driver", "verify", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
assert proc.returncode == 0, f"oracle story failed: exit {proc.returncode}"
print("oracle: an advertised service is found under its app's identity, and no other identity can claim it")
