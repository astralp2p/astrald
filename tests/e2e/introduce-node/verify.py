#!/usr/bin/env python3
"""Oracle: the replacement starts alone, and user.introduce brings it into the swarm.

`first`, the node that generated the words, is stopped (introduce-node-setup).
Before the introduction no member lists `replacement`, and `replacement`
lists itself alone, though the same User claimed it. After it, `member`,
`sibling` and `replacement` list each other in user.swarm_status, never the
expelled `banned`, and list `first` unlinked; each lists the alias the others
give themselves; and `replacement` holds the ban of `banned`. user.info as
the User relayed by `replacement`, a Privileged node, answers with `member`'s
membership, signed by its carrier `first`; relayed by `member`, a Member, it
is refused (D5, D6). The refusals read as the gate and the op answer them.
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
    """The swarm as node lists it: identity → alias, the linked identities, and
    the bans it holds."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        listed = [m for m in await c.user.swarm_status() if m.identity is not None]
        roster = {str(m.identity): str(m.alias) for m in listed}
        linked = {str(m.identity) for m in listed if m.linked}
        bans = {str(e.expulsion.subject) for e in await c.user.list_expelled()
                if e.expulsion is not None and e.expulsion.subject is not None}
    return {"roster": roster, "linked": linked, "bans": bans}


def gaps(views: dict, nodes: dict, first: str) -> list:
    """Every way the trio's views still differ from a converged swarm."""
    ids = {k: nodes[k]["identity"] for k in TRIO}
    banned = nodes["banned"]["identity"]
    out = []
    for k, v in views.items():
        missing = sorted(name for name, i in ids.items() if i not in v["roster"])
        if missing:
            out.append(f"{k} does not list {missing}")
        if first not in v["roster"]:
            out.append(f"{k} does not list the stopped first")
        elif first in v["linked"]:
            out.append(f"{k} lists the stopped first linked")
        if banned in v["roster"]:
            out.append(f"{k} lists the expelled banned")
        if banned not in v["bans"]:
            out.append(f"{k} does not hold the ban of banned")
        for other, i in ids.items():
            own = views[other]["roster"].get(i, "")
            if i in v["roster"] and v["roster"][i] != own:
                out.append(f"{k} names {other} {v['roster'][i]!r}; {other} names itself {own!r}")
    return out


async def converged(nodes: dict, first: str) -> list:
    deadline = time.monotonic() + CONVERGE
    while True:
        views = {k: await view(nodes[k]) for k in TRIO}
        left = gaps(views, nodes, first)
        if not left or time.monotonic() >= deadline:
            return left
        await asyncio.sleep(POLL)


def check_before(f: dict, user: str, nodes: dict) -> None:
    r = nodes["replacement"]["identity"]
    assert "first" not in nodes, "the session view names first, which the world stopped"
    assert f["replacement_user"] == user, (
        f"replacement was claimed for {f['replacement_user']}, not the User {user}")
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


def check_cross_info(f: dict, swarm: dict, nodes: dict) -> None:
    """user.info as the User across the link: from replacement, a Privileged
    node, of member, answered with member's own membership, which its carrier
    `first` signed (a Member's membership is its carrier's, since G7); from
    member, a Member, of replacement, refused, since a Member does not relay
    as the User (D5, D6; the node rejects the relay before an op runs)."""
    i = f["cross_info"]["replacement_of_member"]
    assert i["answered"], f"user.info as the User (replacement_of_member) was refused: {i['detail']}"
    assert i["state"] == "claimed", f"user.info (replacement_of_member) answered state {i['state']!r}"
    assert i["subject"] == nodes["member"]["identity"], (
        f"user.info (replacement_of_member) answered the contract of {i['subject']}, not member")
    assert i["issuer"] == swarm["first"], (
        f"member's membership is signed by {i['issuer']}, not by its carrier first "
        f"{swarm['first']}")
    i = f["cross_info"]["member_of_replacement"]
    assert not i["answered"] and "query rejected" in i["detail"], (
        f"user.info as the User relayed by member, a Member, answered {i}; a Member "
        "does not relay as the User")


async def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]
    f, swarm = facts["introduce_node"], facts["introduce_swarm"]

    check_before(f, swarm["user"], nodes)
    check_answers(f)
    check_cross_info(f, swarm, nodes)
    left = await converged(nodes, swarm["first"])
    assert not left, "the swarm did not converge after the introduction: " + "; ".join(left)

    print("oracle: with first stopped, replacement started alone; after user.introduce "
          "member, sibling and replacement list each other with matching aliases, list "
          "first unlinked, hold the ban; user.info as the User answers relayed by "
          "replacement and is refused relayed by member, a Member")


asyncio.run(main())
