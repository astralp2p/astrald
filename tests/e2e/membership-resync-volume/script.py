#!/usr/bin/env python3
"""Driver: measure the membership re-sync traffic of a 10-node swarm with 5 bans.

1. The User adopts node3..node10 on node1, as adopt-node adopted node2, and
   waits until every node lists ten members.
2. The User expels five fresh identities on node1 and waits until every node
   holds the five bans.
3. Link events: node2 closes its links to node3 three times; after each close
   the driver waits until the link is back and the syncs settle. The deltas of
   the nodes' logs are counted per link event.
4. Sweep: a quiet window of three sweep periods with no link event; the deltas
   are counted per period.

A link-event window also holds the sweeps that fall in it; its own share is
5 ban pushes and 9 contract pushes per sync run (5 bans, the active contract
and 8 sibling contracts).

Counted lines (astrald log, verbosity 1): `syncing with` (one SyncNodesTask
run), `received mod.user.signed_expulsion` and `received mod.auth.signed_contract`
(one accepted push each, on the receiving node).
"""
import asyncio
import re
import time

import astral
from astral.api.crypto import public_key_to_identity

from lib import nodefs
from lib.sessionio import load, write_facts
from lib import swarmstate as ss

NAMES = [f"node{i}" for i in range(1, 11)]
BANS = 5
PERIOD = 10.0      # the reconcile_interval the manifest sets
FLAPS = 3
SETTLE = 8.0       # after a re-link, the time the syncs get to finish
CONVERGE = 150.0
POLL = 1.0

COUNTED = {
    "sync_runs": re.compile(r"syncing with "),
    "ban_pushes": re.compile(r"received mod\.user\.signed_expulsion "),
    "contract_pushes": re.compile(r"received mod\.auth\.signed_contract "),
}


def counts(nodes: list) -> dict:
    """The counted lines over every node's log so far."""
    total = {k: 0 for k in COUNTED}
    for n in nodes:
        text = nodefs.log(n)
        for k, rx in COUNTED.items():
            total[k] += len(rx.findall(text))
    return total


def delta(after: dict, before: dict) -> dict:
    return {k: after[k] - before[k] for k in after}


async def until(probe, budget=CONVERGE):
    deadline = time.monotonic() + budget
    while time.monotonic() < deadline:
        if await probe():
            return True
        await asyncio.sleep(POLL)
    return False


async def adopt(doc, n1, others):
    user_token = doc["facts"]["user_token"]
    for n in others:
        async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
            await c.nodes.add_endpoint(n["identity"], n["lan_endpoint"], experimental=True)
        async with await astral.connect(n["endpoint"], token=n["token"]) as c:
            await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"], experimental=True)
        async with await astral.connect(n1["endpoint"], token=user_token) as c:
            await c.nodes.new_link(n["identity"], endpoint=n["lan_endpoint"],
                                   strategies="basic", experimental=True, timeout=60.0)
        # note: user.adopt is gone (G7); n joins through node1 as a Member
        await ss.admit(n1, user_token, n)


async def all_nodes(nodes, check) -> bool:
    for n in nodes:
        async with await astral.connect(n["endpoint"], token=n["token"]) as c:
            if not await check(c):
                return False
    return True


async def links_to(c, peer_id) -> list:
    return [l.id for l in await c.nodes.links(experimental=True)
            if str(l.remote_identity) == peer_id]


async def main():
    doc = load()
    nodes = [doc["nodes"][name] for name in NAMES]
    n1, n2, n3 = nodes[0], nodes[1], nodes[2]
    ids = {n["identity"] for n in nodes}

    await adopt(doc, n1, nodes[2:])

    async def full_roster(c):
        members = await c.user.swarm_status()
        return ids <= {str(m.identity) for m in members}
    assert await until(lambda: all_nodes(nodes, full_roster)), (
        "the ten nodes never all listed ten members")

    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as c:
        subjects = []
        for _ in range(BANS):
            key = await c.crypto.new_key()
            subject = str(public_key_to_identity(await c.crypto.public_key(key)))
            await c.user.expel(subject)
            subjects.append(subject)

    async def all_bans(c):
        held = {str(e.expulsion.subject) for e in await c.user.list_expelled()}
        return set(subjects) <= held
    assert await until(lambda: all_nodes(nodes, all_bans)), (
        "the five bans never reached every node")

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        linked = [m for m in await c.user.swarm_status() if m.linked]
    linked_from_node2 = len(linked)

    link_events = []
    for _ in range(FLAPS):
        before = counts(nodes)
        start = time.monotonic()
        async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
            closed = await links_to(c, n3["identity"])
            for link_id in closed:
                await c.nodes.close_link(link_id, experimental=True)

            async def relinked():
                return bool(await links_to(c, n3["identity"]))
            back = await until(relinked, 60.0)
            await asyncio.sleep(SETTLE)
            reopened = await links_to(c, n3["identity"])
        d = delta(counts(nodes), before)
        link_events.append({"closed": len(closed), "relinked": back,
                            "links_after": len(reopened),
                            "window_s": round(time.monotonic() - start, 1), **d})
        print(f"driver: link event {len(link_events)}: {link_events[-1]}")

    # a quiet window: no link event is caused, the sweep alone runs
    before = counts(nodes)
    start = time.monotonic()
    await asyncio.sleep(3 * PERIOD)
    d = delta(counts(nodes), before)
    sweep = {"window_s": round(time.monotonic() - start, 1), "periods": 3,
             "linked_from_node2": linked_from_node2, **d}
    print(f"driver: sweep window: {sweep}")

    write_facts({"bans": subjects, "link_events": link_events, "sweep": sweep})


asyncio.run(main())
