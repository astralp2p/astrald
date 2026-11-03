#!/usr/bin/env python3
"""Oracle: the replacement starts alone, and user.introduce brings it into the swarm.

Before the introduction no member lists `replacement`, and `replacement`
lists itself alone, though the same User claimed it. After it, `member`,
`sibling` and `replacement` list each other in user.swarm_status, never the
expelled `banned`; each lists the alias the others give themselves; and
`replacement` holds the ban of `banned`. The refusals read as the gate and
the op answer them.
"""
import asyncio
import time

import astral

from lib.sessionio import load

# why a window: the sync after the introduction, the sibling link to
# `sibling` and the sync back run asynchronously.
CONVERGE = 90.0
POLL = 1.0
TRIO = ("member", "sibling", "replacement")


async def view(node: dict) -> dict:
    """The swarm as node lists it: identity → alias, and the bans it holds."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        roster = {str(m.identity): str(m.alias) for m in await c.user.swarm_status()
                  if m.identity is not None}
        bans = {str(e.expulsion.subject) for e in await c.user.list_expelled()
                if e.expulsion is not None and e.expulsion.subject is not None}
    return {"roster": roster, "bans": bans}


def gaps(views: dict, nodes: dict) -> list:
    """Every way the trio's views still differ from a converged swarm."""
    ids = {k: nodes[k]["identity"] for k in TRIO}
    banned = nodes["banned"]["identity"]
    out = []
    for k, v in views.items():
        missing = sorted(name for name, i in ids.items() if i not in v["roster"])
        if missing:
            out.append(f"{k} does not list {missing}")
        if banned in v["roster"]:
            out.append(f"{k} lists the expelled banned")
        if banned not in v["bans"]:
            out.append(f"{k} does not hold the ban of banned")
        for other, i in ids.items():
            own = views[other]["roster"].get(i, "")
            if i in v["roster"] and v["roster"][i] != own:
                out.append(f"{k} names {other} {v['roster'][i]!r}; {other} names itself {own!r}")
    return out


async def converged(nodes: dict) -> list:
    deadline = time.monotonic() + CONVERGE
    while True:
        views = {k: await view(nodes[k]) for k in TRIO}
        left = gaps(views, nodes)
        if not left or time.monotonic() >= deadline:
            return left
        await asyncio.sleep(POLL)


def check_before(f: dict, nodes: dict) -> None:
    r = nodes["replacement"]["identity"]
    assert f["replacement_user"] == f["user"], (
        f"replacement was claimed for {f['replacement_user']}, not the User {f['user']}")
    for k in ("member", "sibling"):
        assert r not in f["before"][k], f"{k} listed replacement before the introduction"
    assert f["before"]["replacement"] == [r], (
        f"replacement listed {f['before']['replacement']} before the introduction; want itself alone")


def check_answers(f: dict) -> None:
    i = f["introduced"]
    assert not i["refused"] and i["types"] == ["ack"], f"user.introduce as the User answered {i}"
    p = f["probes"]
    assert p["tokenless"]["refused"] and p["tokenless"]["detail"].startswith("Denied"), (
        f"token-less user.introduce answered {p['tokenless']} — want denied by the gate")
    assert p["joined_node"]["refused"] and "not permitted" in p["joined_node"]["detail"], (
        f"user.introduce from a joined node's token answered {p['joined_node']} — want not permitted")
    assert p["self"]["refused"] and "cannot introduce to self" in p["self"]["detail"], (
        f"user.introduce naming the node itself answered {p['self']}")
    assert not p["again"]["refused"] and p["again"]["types"] == ["ack"], (
        f"a second user.introduce from the claimed node's token answered {p['again']}")


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["introduce_node"]

    check_before(f, nodes)
    check_answers(f)
    left = await converged(nodes)
    assert not left, "the swarm did not converge after the introduction: " + "; ".join(left)

    print("oracle: replacement started alone; after user.introduce member, sibling and "
          "replacement list each other with matching aliases and hold the ban")


asyncio.run(main())
