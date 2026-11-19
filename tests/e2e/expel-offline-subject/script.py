#!/usr/bin/env python3
"""Driver: node3 carries a swarm follow to node1 and node2 before it goes down.

1. All three nodes agree on the whole roster.
2. The follower (follower/, a long-lived app by lib/apps.py) follows a service
   with reach swarm on node3; it outlives this driver and node3's restart.
3. node1's and node2's logs are tapped until each logged a services.discover
   with node3: node3's carries reach both members, so the path the oracle
   judges after the ban is live.

The runner then stops node3, `while_down.py` has the User expel node3 on node1,
and node3 starts again.
"""
import asyncio
import os
import time
from contextlib import AsyncExitStack

import astral

from lib import apps
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

PKG = "tests/e2e/expel-offline-subject/follower"
NAME = "expel-offline-subject-follower"
CONVERGE = 30.0    # how long the three nodes may take to link each other
CARRY = 60.0       # how long node3's carries may take to reach both members
POLL = 0.5


async def whole(node: dict, roster: set) -> bool:
    """Whether node lists the three nodes and links the other two."""
    want_linked = roster - {node["identity"]}
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        deadline = time.monotonic() + CONVERGE
        while True:
            members = await c.user.swarm_status()
            listed = {str(m.identity) for m in members if m.identity is not None}
            linked = {str(m.identity) for m in members if m.linked and m.identity is not None}
            if listed == roster and linked == want_linked:
                return True
            if time.monotonic() >= deadline:
                return False
            await asyncio.sleep(POLL)


async def carried(taps: dict, id3: str, follower) -> dict:
    """Waits until each tapped member logged a services.discover with node3."""
    deadline = time.monotonic() + CARRY
    while True:
        seen = {n: any(id3 in e["text"] for e in t.find("services.discover"))
                for n, t in taps.items()}
        if all(seen.values()) or time.monotonic() >= deadline:
            return seen
        if not follower.alive():
            raise RuntimeError(f"the follower exited:\n{follower.log_tail()}")
        await asyncio.sleep(POLL)


async def main():
    doc = load()
    nodes = {n: doc["nodes"][n] for n in ("node1", "node2", "node3")}
    roster = {n["identity"] for n in nodes.values()}
    id3 = nodes["node3"]["identity"]

    for name, node in nodes.items():
        assert await whole(node, roster), (
            f"{name} does not list and link the whole three-node roster "
            f"within {CONVERGE:.0f} s; the world is not whole")

    async with AsyncExitStack() as stack:
        taps = {n: await stack.enter_async_context(LogTap(nodes[n]))
                for n in ("node1", "node2")}
        binary = apps.build_go(PKG)
        follower = apps.spawn(NAME, [binary, os.environ["ASTRAL_TESTS_SESSION"]])
        state = follower.wait_state(lambda s: s.get("boundaries", 0) >= 1, timeout=60)
        seen = await carried(taps, id3, follower)
        tap_state = {n: t.state() for n, t in taps.items()}

    write_facts({"offline_subject": {
        "follower": NAME,
        "app": state["app"],
        "node3_restarts_before": nodes["node3"].get("restarts") or 0,
        "discover_before": seen,
        "taps_before": tap_state,
    }})
    print(f"driver: follower app {state['app'][:12]}… follows on node3; "
          f"services.discover with node3 seen on {seen}")


asyncio.run(main())
