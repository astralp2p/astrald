#!/usr/bin/env python3
"""Driver: how a discovery comes back to a stranger, and across the swarm.

The driver acts and judges nothing. It links the stranger to node1 and node2,
then runs a Go program (driver/) that records, for a service node2 answers and one it
never answers: what a swarm member and the stranger each get from node2, what
the stranger gets asking node1 to carry a discovery, and what an app on node1
gets discovering across the swarm. The oracle reads that record.

why the flow is in Go: astral-py speaks the pre-v1 Services wire.
why a stranger of its own: `stranger` of swarm-authz-matrix is linked to node1
only, and a test that continued matrix would take it out of the shared world
(the planner forks a test's parent when another test continues it). A fresh
node is as much a stranger: node2 judges the identity, not the node's state.
why the stranger dials and node1 and node2 learn no endpoint for it: it should
leave them holding no record they did not have.
why the stranger's links stay open: no later test in the world asserts an exact
link set, and the oracle needs them for its own round trip.
"""
import asyncio
import json
import os
import subprocess
import sys
from pathlib import Path

import astral

from lib.sessionio import load, write_facts

ROOT = Path(__file__).resolve().parents[3]


async def link_stranger(peers: list, s: dict) -> None:
    async with await astral.connect(s["endpoint"], token=s["token"]) as c:
        for peer in peers:
            await c.nodes.add_endpoint(peer["identity"], peer["lan_endpoint"],
                                       experimental=True)
            await c.nodes.new_link(peer["identity"], endpoint=peer["lan_endpoint"],
                                   strategies="basic", experimental=True,
                                   timeout=60.0)


nodes = load()["nodes"]
asyncio.run(link_stranger([nodes["node1"], nodes["node2"]], nodes["ssor_stranger"]))

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-swarm-outcome-rules/driver", "drive",
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
print("driver: stranger and swarm discovery outcomes recorded")
