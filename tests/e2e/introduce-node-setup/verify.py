#!/usr/bin/env python3
"""Oracle: the swarm stands without `first`, which the world stopped.

`first` is gone from the session view. `member` and `sibling` list each
other and `first` in user.swarm_status, `first` unlinked, never the expelled
`banned`, and each holds the ban of `banned`.
"""
import asyncio
import time

import astral

from lib.sessionio import load

# why a window: a link to a stopped node drops when its keepalive fails, not
# at the stop.
CONVERGE = 90.0
POLL = 1.0
PAIR = ("member", "sibling")


async def view(node: dict) -> dict:
    """The swarm as node lists it: identity → linked, and the bans it holds."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        roster = {str(m.identity): bool(m.linked) for m in await c.user.swarm_status()
                  if m.identity is not None}
        bans = {str(e.expulsion.subject) for e in await c.user.list_expelled()
                if e.expulsion is not None and e.expulsion.subject is not None}
    return {"roster": roster, "bans": bans}


def gaps(views: dict, nodes: dict, first: str) -> list:
    """Every way the pair's views still differ from a swarm without first."""
    ids = {k: nodes[k]["identity"] for k in PAIR}
    banned = nodes["banned"]["identity"]
    out = []
    for k, v in views.items():
        missing = sorted(name for name, i in ids.items() if i not in v["roster"])
        if missing:
            out.append(f"{k} does not list {missing}")
        if first not in v["roster"]:
            out.append(f"{k} does not list first")
        elif v["roster"][first]:
            out.append(f"{k} still lists first linked")
        if banned in v["roster"]:
            out.append(f"{k} lists the expelled banned")
        if banned not in v["bans"]:
            out.append(f"{k} does not hold the ban of banned")
    return out


async def settled(nodes: dict, first: str) -> list:
    deadline = time.monotonic() + CONVERGE
    while True:
        views = {k: await view(nodes[k]) for k in PAIR}
        left = gaps(views, nodes, first)
        if not left or time.monotonic() >= deadline:
            return left
        await asyncio.sleep(POLL)


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["introduce_swarm"]

    assert "first" not in nodes, "the session view still names first after the stop"
    assert f["words"], "the driver recorded no words"
    left = await settled(nodes, f["first"])
    assert not left, "the swarm without first is not as built: " + "; ".join(left)

    print("oracle: first is stopped; member and sibling list each other and first "
          "unlinked, and hold the ban")


asyncio.run(main())
