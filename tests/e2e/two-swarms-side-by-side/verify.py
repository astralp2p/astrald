#!/usr/bin/env python3
"""Oracle: two Users' swarms side by side, one link between them, each swarm
exactly its own.

User A's swarm is node1 and node2; User B's is tssb1 and tssb2; node2 holds a
live link to tssb1. The oracle reads every node as a member of its own swarm
and judges four things, collecting every failure before it exits:

1. Positive: each swarm is whole. Within CONVERGE seconds every node lists its
   own swarm's other node as a linked sibling, and node2 holds its link to
   tssb1. Without this the rest would pass on four lone nodes.
2. Exactness over a window: for WINDOW seconds, at every poll, each node's
   `user.list_siblings` is exactly the other node of its swarm and its
   `user.swarm_status` is exactly its own two nodes, both linked. Nothing of
   the other swarm appears, and node2 and tssb1 stay linked throughout.
3. No leakage: each node still reports its own User, node1 and tssb2 hold no
   link to the other swarm, and across the link node2 and tssb1 are refused
   the other swarm's `user.list_siblings` and `user.swarm_status` (SeeSwarm
   grants a member of the asked node's own swarm, not any linked node).
4. No foreign contract: of the four contracts the driver offered, the two
   crossing the swarms were refused and the two inside a swarm were taken
   (mod/user/src/object_receiver.go checkNodeContract).
"""
import asyncio
import contextlib
import time

import astral

from lib.sessionio import load

CONVERGE = 30.0
WINDOW = 15.0
POLL = 1.0
DEADLINE = 15


def ids(members) -> set:
    return {str(m.identity) for m in members if m.identity is not None}


def linked(members) -> set:
    return {str(m.identity) for m in members
            if m.linked and m.identity is not None}


async def snapshot(c) -> dict:
    """One node's view of its own swarm, as that node's member reads it."""
    info = await c.user.info()
    status = await c.user.swarm_status()
    return {
        "user": str(info.user_id),
        "siblings": {str(i) for i in await c.user.list_siblings()},
        "members": ids(status),
        "linked": linked(status),
        "links": {str(l.remote_identity)
                  for l in await c.nodes.links(experimental=True)
                  if l.remote_identity is not None},
    }


def judge(who: str, snap: dict, user: str, swarm: set, me: str) -> list:
    """What one snapshot says against the swarm the node belongs to."""
    peers = swarm - {me}
    bad = []
    if snap["user"] != user:
        bad.append(f"{who} reports User {snap['user']}, not {user}")
    if snap["siblings"] != peers:
        bad.append(f"{who} list_siblings is {sorted(snap['siblings'])}, "
                   f"not exactly {sorted(peers)}")
    if snap["members"] != swarm:
        bad.append(f"{who} swarm_status members are {sorted(snap['members'])}, "
                   f"not exactly {sorted(swarm)}")
    if not peers <= snap["linked"]:
        bad.append(f"{who} swarm_status does not show "
                   f"{sorted(peers - snap['linked'])} linked")
    return bad


async def refusal(coro) -> str | None:
    """The refusal text of a query that must be refused; None when it was answered."""
    try:
        await asyncio.wait_for(coro, DEADLINE)
    except Exception as e:  # noqa: BLE001 — the text is judged by the caller
        return f"{type(e).__name__}: {e}"
    return None


async def main():
    doc = load()
    nodes, facts = doc["nodes"], doc["facts"]
    n1, n2, b1, b2 = (nodes[n] for n in ("node1", "node2", "tssb1", "tssb2"))
    a_id, b_id = facts["user_id"], facts["b_user_id"]
    swarm_a = {n1["identity"], n2["identity"]}
    swarm_b = {b1["identity"], b2["identity"]}
    assert a_id != b_id, "the two swarms belong to one User"
    assert not swarm_a & swarm_b, "the two swarms share a node"

    # name, node, the token that reads it, its User, its swarm, the other swarm
    askers = [
        ("node1", n1, facts["user_token"], a_id, swarm_a, swarm_b),
        ("node2", n2, n2["token"], a_id, swarm_a, swarm_b),
        ("tssb1", b1, facts["b_user_token"], b_id, swarm_b, swarm_a),
        ("tssb2", b2, b2["token"], b_id, swarm_b, swarm_a),
    ]
    # the one link allowed across the swarms: node2 <-> tssb1
    bridge = {"node2": {b1["identity"]}, "tssb1": {n2["identity"]}}
    failures = []

    def verdicts(snaps: dict, label: str) -> list:
        bad = [f"{label}{m}" for name, n, _t, user, swarm, _f in askers
               for m in judge(name, snaps[name], user, swarm, n["identity"])]
        for name, _n, _t, _u, _s, foreign in askers:
            crossing = (snaps[name]["links"] & foreign) - bridge.get(name, set())
            if crossing:
                bad.append(f"{label}{name} is linked to {sorted(crossing)} "
                           "of the other swarm")
        if b1["identity"] not in snaps["node2"]["links"]:
            bad.append(f"{label}node2 holds no link to tssb1")
        if n2["identity"] not in snaps["tssb1"]["links"]:
            bad.append(f"{label}tssb1 holds no link to node2")
        return bad

    async with contextlib.AsyncExitStack() as stack:
        conns = {}
        for name, n, token, *_ in askers:
            conns[name] = await stack.enter_async_context(
                await astral.connect(n["endpoint"], token=token))

        async def sweep() -> dict:
            return {name: await snapshot(conns[name]) for name, *_ in askers}

        # 1. positive: both swarms whole, exact, and the bridge up
        deadline = time.monotonic() + CONVERGE
        bad = verdicts(await sweep(), "")
        while bad and time.monotonic() < deadline:
            await asyncio.sleep(POLL)
            bad = verdicts(await sweep(), "")
        failures += [f"after {CONVERGE:.0f}s: {m}" for m in bad]

        # 2. the window: exact at every poll
        polls = 0
        end = time.monotonic() + WINDOW
        while time.monotonic() < end and not failures:
            await asyncio.sleep(POLL)
            polls += 1
            failures += verdicts(await sweep(), f"poll {polls}: ")

        # 3. SeeSwarm across the bridge: refused both ways. A node asks as
        # itself, so the answer is the asked node's own SeeSwarm refusal; B's
        # User asks through tssb1, a relay for a User the asked node does not
        # serve, which the node rejects before an op runs.
        b1_node = await stack.enter_async_context(
            await astral.connect(b1["endpoint"], token=b1["token"]))
        probes = (("node2", conns["node2"], b1, ("not permitted",)),
                  ("tssb1", b1_node, n2, ("not permitted",)),
                  ("User B via tssb1", conns["tssb1"], n2,
                   ("not permitted", "query rejected")))
        for who, c, target, marks in probes:
            for op in ("list_siblings", "swarm_status"):
                text = await refusal(getattr(c.user, op)(target=target["identity"]))
                if text is None:
                    failures.append(f"{who} was answered {op} by a node of the "
                                    "other swarm")
                elif not any(m in text.lower() for m in marks):
                    failures.append(f"{who} {op} across the bridge was refused as "
                                    f"{text!r}, not as {' or '.join(marks)}")

    # 4. contracts the driver offered
    pushes = facts["pushes"]
    for key in ("a_contract_to_b_node", "b_contract_to_a_node"):
        if pushes.get(key) is not False:
            failures.append(f"{key}: a node took a contract issued by the other "
                            f"User (push answered {pushes.get(key)!r})")
    for key in ("a_contract_to_a_node", "b_contract_to_b_node"):
        if pushes.get(key) is not True:
            failures.append(f"{key}: a node refused a contract of its own swarm "
                            f"(push answered {pushes.get(key)!r}), so the "
                            "refusals prove nothing")

    if failures:
        raise SystemExit("oracle: " + "; ".join(failures))
    print(f"oracle: swarms {sorted(swarm_a)} and {sorted(swarm_b)} exact over "
          f"{polls} polls, bridged once, no foreign contract, no SeeSwarm across it")


asyncio.run(main())
