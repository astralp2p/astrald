#!/usr/bin/env python3
"""Driver: a swarm follow across a late join and an expulsion.

The fresh node ssm_late is linked to node1 but not adopted. The Go program
(services-swarm/driver, membership mode) opens the follow, adopts ssm_late
as the User and expels it, and records which steps held.

why the flow is in Go: astral-py speaks the pre-v1 Services wire.
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


async def link(n1: dict, late: dict, user_token: str) -> None:
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.nodes.add_endpoint(late["identity"], late["lan_endpoint"], experimental=True)
    async with await astral.connect(late["endpoint"], token=late["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"], experimental=True)
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        await c.nodes.new_link(late["identity"], endpoint=late["lan_endpoint"],
                               strategies="basic", experimental=True, timeout=60.0)


doc = load()
asyncio.run(link(doc["nodes"]["node1"], doc["nodes"]["ssm_late"], doc["facts"]["user_token"]))

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-swarm/driver", "membership", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false")},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: swarm follow tracked a late join and an expulsion")
