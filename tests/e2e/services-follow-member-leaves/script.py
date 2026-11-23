#!/usr/bin/env python3
"""Driver: an open swarm follow across a member's expiry and its rejoin.

The fresh node sfml_member is linked to node1 but not adopted. The Go program
(services-swarm/driver, rejoin mode) opens a `reach=swarm` follow of the
service `sfml` on node1, admits sfml_member with a membership contract that
expires in 60 s while its provider advertises `sfml`, waits until the follow
drops the member after the expiry, then signs a second contract (1 h) with
the User's key and the member's node key, tries `user.accept_contract` with
it on the member, pushes it to node1, and waits until the follow shows the
member's offering again. It prints the `follow_member_leaves` fact.

why the flow is in Go: astral-py speaks the pre-v1 Services wire.
why the node key: every op that collects a node's subject signature refuses
a contract shorter than an hour, so the driver signs as sfml_member itself.
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


async def link(n1: dict, member: dict, user_token: str) -> None:
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.nodes.add_endpoint(member["identity"], member["lan_endpoint"], experimental=True)
    async with await astral.connect(member["endpoint"], token=member["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"], experimental=True)
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        await c.nodes.new_link(member["identity"], endpoint=member["lan_endpoint"],
                               strategies="basic", experimental=True, timeout=60.0)


doc = load()
member = doc["nodes"]["sfml_member"]
asyncio.run(link(doc["nodes"]["node1"], member, doc["facts"]["user_token"]))
key = nodefs.read_bytes(member, "data/node_key").hex()

proc = subprocess.run(
    ["go", "run", "./tests/e2e/services-swarm/driver", "rejoin", os.environ["ASTRAL_TESTS_SESSION"]],
    cwd=ROOT, capture_output=True, text=True,
    env={**os.environ, "GOFLAGS": os.environ.get("GOFLAGS", "-buildvcs=false"),
         "MEMBER_NODE_KEY": key})
sys.stderr.write(proc.stderr)
lines = [l for l in proc.stdout.splitlines() if l.startswith("{")]
if lines:
    write_facts(json.loads(lines[-1]))
if proc.returncode != 0:
    sys.exit(f"driver: exit {proc.returncode}")
print("driver: the swarm follow tracked the member's expiry and its rejoin")
