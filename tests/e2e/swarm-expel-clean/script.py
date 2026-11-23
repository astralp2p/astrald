#!/usr/bin/env python3
"""Driver: four siblings' mesh apps talk, then the User expels node4.

1. Taps every node's log (`lib/nodelog.py`) across the whole flow.
2. Spawns the `f4` mesh apps on node1-node4 (`lib/meshapps.py`, period 1 s,
   `keep_removed`, so each app keeps calling node4's app after its follow
   removed it) and waits up to 60 s until all 12 pairs answer.
3. The User expels node4 on node1, and the flow holds HOLD seconds after the
   expel. node1-node3 are polled for the ban every 0.5 s meanwhile.
4. At the end each member's links to node4 are listed, and the apps stop;
   their state files and call logs stay for the oracle.

The driver judges nothing. It records the `expel_clean` fact: {spawned_at,
ready_at (null when the 12 pairs did not answer within 60 s), missing,
expel_at, hold_end, ban_seen (when each member's user.list_expelled first
named node4, host clock), logs (the kept entries per node), taps, links}.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import linkview, meshapps
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

NAMES = ["node1", "node2", "node3", "node4"]
MEMBERS = ["node1", "node2", "node3"]
PREFIX = "f4"
READY = 60.0
# why 30 s: the watch window of the E2E tests exhibit's F4 (no push, discovery
# or link crosses the ban for 30 s)
HOLD = 30.0
POLL = 0.5
# the entries that tell a push, a discovery, a relay or a link, the moments
# the ban landed, and the ban answers (the needles of expel-cross-traffic)
KEEP = ("services.discover", "rejecting pushed contract", "push relay contract",
        "refused relay", "removing sibling", "expelled", "-link with",
        "answered its ban")
# astrald mod/user/src/expel.go leaveSwarm: the expelled node's own removal
LEFT = "this node is expelled"


def kept(tap: LogTap, others: list) -> list:
    """The tap's entries that match KEEP and name one of others, or tell the
    node's own expulsion."""
    return [{"t": e["t"], "text": e["text"]} for e in tap.find(*KEEP)
            if LEFT in e["text"] or any(o in e["text"] for o in others)]


async def banned(c, subject: str) -> bool:
    try:
        return any(e.expulsion is not None and str(e.expulsion.subject) == subject
                   for e in await c.user.list_expelled())
    except Exception:  # noqa: BLE001 — an unanswered poll is a poll that saw nothing
        return False


async def hold(clients: dict, id4: str, until: float) -> dict:
    """Poll the members for the ban until `until`; {member: first seen}."""
    seen = {}
    while time.time() < until:
        for name in MEMBERS:
            if name not in seen and await banned(clients[name], id4):
                seen[name] = time.time()
        await asyncio.sleep(POLL)
    return seen


async def main():
    doc = load()
    nodes = doc["nodes"]
    ids = {n: nodes[n]["identity"] for n in NAMES}
    id4 = ids["node4"]
    facts = {}
    async with AsyncExitStack() as stack:
        taps = {n: await stack.enter_async_context(LogTap(nodes[n])) for n in NAMES}
        clients = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in MEMBERS}

        # why threads: the meshapps calls block, and the taps must keep reading
        # their streams meanwhile
        await asyncio.to_thread(meshapps.binary)
        facts["spawned_at"] = time.time()
        await asyncio.to_thread(meshapps.spawn, PREFIX, nodes, NAMES, service=PREFIX,
                                period=1.0, keep_removed=True)
        await asyncio.to_thread(meshapps.wait_ready, PREFIX, NAMES)
        facts["ready_at"], missing = await asyncio.to_thread(
            meshapps.wait_answering, PREFIX, NAMES, facts["spawned_at"], READY)
        facts["missing"] = [f"{c}->{p}" for c, p in missing]

        if facts["ready_at"] is not None:
            async with await astral.connect(nodes["node1"]["endpoint"],
                                            token=doc["facts"]["user_token"]) as cu:
                await cu.user.expel(id4)
            facts["expel_at"] = time.time()
            facts["hold_end"] = facts["expel_at"] + HOLD
            facts["ban_seen"] = await hold(clients, id4, facts["hold_end"])
            facts["links"] = {n: sorted(linkview.ids(await linkview.links(clients[n]), id4))
                              for n in MEMBERS}
        # why the pause: the tap streams entries a moment after a node logs them
        await asyncio.sleep(1.0)
        facts["logs"] = {n: kept(taps[n], [id4]) for n in MEMBERS}
        facts["logs"]["node4"] = kept(taps["node4"], [ids[n] for n in MEMBERS])
        facts["taps"] = {n: t.state() for n, t in taps.items()}
    meshapps.stop(PREFIX, NAMES)

    write_facts({"expel_clean": facts})
    if facts["ready_at"] is None:
        print(f"driver: the 12 pairs did not answer within {READY:.0f} s; missing "
              f"{facts['missing']}; node4 not expelled")
        return
    print(f"driver: 12 pairs answered {facts['ready_at'] - facts['spawned_at']:.1f} s after "
          f"the spawn; node4 expelled, held {HOLD:.0f} s; ban seen on "
          f"{sorted(facts['ban_seen'])}")


asyncio.run(main())
