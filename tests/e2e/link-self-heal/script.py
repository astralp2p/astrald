#!/usr/bin/env python3
"""Driver: cut one sibling pair's links five times, and watch the swarm heal.

The world holds node1, node2 and node3 in one swarm, each linked to the other
two (swarm-roster-sync). Each node runs a MaintainLinkTask per sibling
(astrald mod/user/src/module.go runSiblingLinker), which a LinkClosedEvent
with no link left wakes (mod/user/src/maintain_link_task.go ReceiveEvent).

Five cycles, each on one pair A-B, closed on C:
node1-node2 on node1, node1-node2 on node2, node1-node2 on node1,
node1-node3 on node1, node2-node3 on node2. A cycle:

1. marks A's and B's log taps;
2. lists C's links to its peer and closes each listed id once with
   nodes.close_link; the peer may redial within milliseconds, so the driver
   never loops until none remain, which could chase the new link;
3. polls local ops only, every 0.25 s for up to 30 s, until A and B both hold
   a link to each other whose id was not closed, and both mark the other
   linked in user.swarm_status;
4. after 1 s, keeps A's and B's maintain-task log lines about the peer, and
   snapshots the link ids of all three pairs from both ends.

A cycle that does not heal ends the cycles.

why only local ops: a query aimed at the peer would make the router dial on
demand (astrald mod/nodes/src/query_router.go RouteQuery) and heal a pair
whose maintain task is dead.

The driver judges nothing. It records the `link_heal` fact for the oracle.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import linkview
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

NAMES = ("node1", "node2", "node3")
PAIRS = (("node1", "node2"), ("node1", "node3"), ("node2", "node3"))
# (A, B, the node that closes the links)
CYCLES = (("node1", "node2", "node1"), ("node1", "node2", "node2"),
          ("node1", "node2", "node1"), ("node1", "node3", "node1"),
          ("node2", "node3", "node2"))
CONVERGE = 30.0
HEAL = 30.0
POLL = 0.25
# the maintain task's own lines (maintain_link_task.go Run), each after
# "link to <peer> ": a task that started linked logs "established" on its
# first relink and nothing before it
WAKE = ("is broken, trying to reconnect", "established", "restored after")
NOT_FOUND = "link not found"


def pair_key(a: str, b: str) -> str:
    return f"{a}-{b}"


async def linked(c) -> set:
    return {str(m.identity) for m in await c.user.swarm_status()
            if m.linked and m.identity is not None}


class Swarm:
    """One session per node, on the node's own token."""

    def __init__(self, nodes: dict, clients: dict):
        self.nodes, self.c = nodes, clients
        self.ids = {n: nodes[n]["identity"] for n in NAMES}

    async def links(self) -> dict:
        return {n: await linkview.links(self.c[n]) for n in NAMES}

    async def snapshot(self) -> dict:
        """pair -> {node: sorted ids of its links to the other}, both ends."""
        rows = await self.links()
        return {pair_key(a, b): {a: sorted(linkview.ids(rows[a], self.ids[b])),
                                 b: sorted(linkview.ids(rows[b], self.ids[a]))}
                for a, b in PAIRS}

    async def whole(self) -> bool:
        for n in NAMES:
            members = await self.c[n].user.swarm_status()
            if {str(m.identity) for m in members} != set(self.ids.values()):
                return False
            if await linked(self.c[n]) != set(self.ids.values()) - {self.ids[n]}:
                return False
        return True


async def baseline(sw: Swarm) -> dict:
    """The swarm whole, then two snapshots 3 s apart, until they agree."""
    deadline = time.monotonic() + CONVERGE
    while not await sw.whole() and time.monotonic() < deadline:
        await asyncio.sleep(1.0)
    whole = await sw.whole()
    first = await sw.snapshot()
    for _ in range(5):
        await asyncio.sleep(3.0)
        second = await sw.snapshot()
        if second == first:
            return {"whole": whole, "quiet": True, "pairs": second}
        first = second
    return {"whole": whole, "quiet": False, "pairs": first}


async def cut(sw: Swarm, closer: str, peer: str) -> dict:
    """Close each link closer lists to peer, once."""
    c = sw.c[closer]
    listed = sorted(linkview.ids(await linkview.links(c), sw.ids[peer]))
    outcomes = {}
    for link_id in listed:
        try:
            await c.nodes.close_link(link_id, experimental=True)
            outcomes[link_id] = "closed"
        except astral.errors.RemoteError as e:
            if NOT_FOUND not in e.message:
                raise
            outcomes[link_id] = NOT_FOUND
    return {"closed": listed, "outcomes": outcomes}


async def heal(sw: Swarm, a: str, b: str, closed: set, t0: float) -> dict:
    """Poll a's and b's own views until they agree on a link not closed."""
    ida, idb = sw.ids[a], sw.ids[b]
    deadline = t0 + HEAL
    while True:
        la = await linkview.links(sw.c[a])
        lb = await linkview.links(sw.c[b])
        agreed = (linkview.ids(la, idb) & linkview.ids(lb, ida)) - closed
        up = (idb in await linked(sw.c[a]) and ida in await linked(sw.c[b]))
        now = time.monotonic()
        if agreed and up:
            return {"healed_after": round(now - t0, 3),
                    "agreed": sorted(agreed),
                    "links": {a: linkview.to(la, idb), b: linkview.to(lb, ida)}}
        if now >= deadline:
            return {"healed_after": None, "agreed": [], "swarm_linked": up,
                    "links": {a: linkview.to(la, idb), b: linkview.to(lb, ida)}}
        await asyncio.sleep(POLL)


def wakes(tap: LogTap, mark: int, peer: str) -> list:
    lines = tuple(f"link to {peer} {w}" for w in WAKE)
    return [e for e in tap.entries[mark:]
            if any(e["text"].startswith(l) for l in lines)]


async def main():
    doc = load()
    nodes = {n: doc["nodes"][n] for n in NAMES}
    async with AsyncExitStack() as stack:
        clients = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in NAMES}
        taps = {n: await stack.enter_async_context(LogTap(nodes[n]))
                for n in NAMES}
        sw = Swarm(nodes, clients)

        base = await baseline(sw)
        print(f"driver: baseline whole={base['whole']} quiet={base['quiet']} "
              f"{base['pairs']}")

        cycles = []
        for i, (a, b, closer) in enumerate(CYCLES, 1):
            peer = b if closer == a else a
            marks = {a: len(taps[a].entries), b: len(taps[b].entries)}
            t0 = time.monotonic()
            done = await cut(sw, closer, peer)
            healed = await heal(sw, a, b, set(done["closed"]), t0)
            await asyncio.sleep(1.0)
            log = {a: wakes(taps[a], marks[a], sw.ids[b]),
                   b: wakes(taps[b], marks[b], sw.ids[a])}
            snap = await sw.snapshot()
            cycles.append({"cycle": i, "pair": pair_key(a, b),
                           "closer": closer, **done, **healed,
                           "log": log, "pairs": snap})
            print(f"driver: cycle {i} {a}-{b} closed on {closer}: "
                  f"{len(done['closed'])} link(s) closed, healed after "
                  f"{healed['healed_after']} s, agreed {healed['agreed']}, "
                  f"wake lines {sum(len(v) for v in log.values())}")
            if healed["healed_after"] is None:
                # why: the oracle fails on this cycle; cutting more pairs of
                # a broken swarm would only run the driver out of time
                break
            await asyncio.sleep(2.0)

        await asyncio.sleep(5.0)
        final = await sw.snapshot()
        print(f"driver: final {final}")
        drops = {n: [e for e in taps[n].entries
                     if "log.listen: dropped" in e["text"]] for n in NAMES}
        tap_state = {n: taps[n].state() for n in NAMES}

    write_facts({"link_heal": {
        "ids": sw.ids, "baseline": base, "cycles": cycles, "final": final,
        "taps": tap_state, "drops": drops}})


asyncio.run(main())
