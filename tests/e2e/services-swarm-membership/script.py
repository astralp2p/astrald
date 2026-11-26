#!/usr/bin/env python3
"""Driver: a swarm follow across a late join, an expulsion and an expiry.

The fresh nodes ssm_late and ssm_expiry are linked to node1 but not members.
The Go program (services-swarm/driver, membership mode) opens the follow,
admits ssm_late through node1 and expels it, admits ssm_expiry with a contract
that expires in 90 seconds, and records which steps held.

why the flow is in Go: astral-py speaks the pre-v1 Services wire.
"""
import asyncio
import json
import os
import subprocess
import sys
from pathlib import Path

import astral

from lib import nodefs
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
for name in ("ssm_late", "ssm_expiry"):
    asyncio.run(link(doc["nodes"]["node1"], doc["nodes"][name], doc["facts"]["user_token"]))

# why the node key: every op that collects a node's subject signature refuses
# a contract shorter than an hour, so the driver signs as ssm_expiry itself.
expiry_key = nodefs.read_bytes(doc["nodes"]["ssm_expiry"], "data/node_key").hex()

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-swarm/driver", "membership", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false"),
         "EXPIRY_NODE_KEY": expiry_key},
)
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: swarm follow tracked a late join, an expulsion and an expiry")
