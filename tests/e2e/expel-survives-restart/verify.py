#!/usr/bin/env python3
"""Oracle: after all three nodes restart, the ban still holds.

The runner restarts node1, node2 and node3 between driver and oracle
(test.toml `restart`), and the session view counts each restart. Every check
is the oracle's own read, on the node's own token (the question is what that
node itself believes) or on the User's token for node1:

- each node was restarted exactly once since the driver ran;
- node1 and node2 still hold exactly one ban, the User's on node3
  (`user.list_expelled`): bans live in the database, not in memory;
- each remaining member's `user.swarm_status` lists exactly node1 and node2,
  and, within a window, `user.list_siblings` lists exactly the other one and
  the roster marks it linked: the two members re-form the swarm without node3;
- node3 reads itself `unclaimed`, never `claimed` (its cleared contract
  stayed cleared) and never `fresh` (the ownership history stayed, see
  mod/sys state.go NodeState), and its own gate denies `user.swarm_status`;
- node3 cannot link back in: its `user.join` to node1 and the User's
  `user.adopt` of node3 are refused as expelled, and afterwards no roster has
  node3 and node3 is still `unclaimed`.

Exact sets throughout: a ban that came back short, a node3 that came back as a
member, or a roster with an extra node fails, not only an empty one.
"""
import asyncio
import time

import astral

from lib.sessionio import load

# why a window: every link died with its process; node1 and node2 re-link
# through their maintain-link tasks (backoff from 1 s) once each has read its
# stored contract.
CONVERGE = 60.0
POLL = 1.0
EXPELLED = "identity expelled from swarm"


def names(ids, by_id) -> str:
    return "[" + ", ".join(by_id.get(i, i[:12] + "…") for i in sorted(ids)) + "]"


def bans(expelled) -> dict:
    """subject -> issuer of every ban in a list_expelled answer."""
    return {str(e.expulsion.subject): str(e.expulsion.issuer)
            for e in expelled
            if e.expulsion is not None and e.expulsion.subject is not None}


async def refusal(coro) -> Exception | None:
    """The exception coro raised, or None when it answered."""
    try:
        await asyncio.wait_for(coro, 20)
    except Exception as e:  # noqa: BLE001 — the refusal is what is judged
        return e
    return None


async def observe(c) -> dict:
    members = await c.user.swarm_status()
    sibs = await c.user.list_siblings()
    return {
        "roster": {str(m.identity) for m in members},
        "linked": {str(m.identity) for m in members if m.linked},
        "siblings": {str(s) for s in sibs},
    }


async def main():
    doc = load()
    facts = doc["facts"]
    nodes = {n: doc["nodes"][n] for n in ("node1", "node2", "node3")}
    ids = {n: nodes[n]["identity"] for n in nodes}
    by_id = {v: k for k, v in ids.items()}
    user_id, user_token = facts["user_id"], facts["user_token"]
    id1, id2, id3 = ids["node1"], ids["node2"], ids["node3"]
    remaining = {id1, id2}
    assert set(facts["members_before"]) == remaining, (
        f"the recorded members {facts['members_before']} are not node1+node2")
    assert facts["expelled_id"] == id3, (
        f"expelled_id {facts['expelled_id']} is not node3 {id3}")

    for name, node in nodes.items():
        n = (node.get("restarts") or 0) - facts["restarts_before"][name]
        assert n == 1, (
            f"{name} was restarted {n} times between driver and oracle, "
            "not once")

    # --- node1 and node2: the ban and the swarm without node3 ------------
    for name, token in (("node1", user_token), ("node2", nodes["node2"]["token"])):
        other = ({id1, id2} - {ids[name]})
        async with await astral.connect(nodes[name]["endpoint"],
                                        token=token) as c:
            b = bans(await c.user.list_expelled())
            assert b == {id3: user_id}, (
                f"{name} holds bans {b} after the restart, not exactly the "
                f"User's ban on node3 {id3} (bans not loaded from storage)")
            deadline = time.monotonic() + CONVERGE
            got = await observe(c)
            while ((got["roster"] != remaining or got["linked"] != other
                    or got["siblings"] != other)
                   and time.monotonic() < deadline):
                await asyncio.sleep(POLL)
                got = await observe(c)
            assert got["roster"] == remaining, (
                f"{name}'s swarm_status lists {names(got['roster'], by_id)}, "
                f"not exactly node1+node2, {CONVERGE:.0f} s after the restart "
                "(the expelled node is back in the roster, or a member is "
                "missing)")
            assert got["siblings"] == other, (
                f"{name}'s list_siblings is {names(got['siblings'], by_id)}, "
                f"not exactly {names(other, by_id)}, {CONVERGE:.0f} s after "
                "the restart")
            assert got["linked"] == other, (
                f"{name}'s swarm_status marks {names(got['linked'], by_id)} "
                f"linked, not exactly {names(other, by_id)}")

    # --- node3: still out, still owned-before -----------------------------
    async def node3_view(c):
        info = await c.user.info()
        return info.state, await refusal(c.user.swarm_status())

    async with await astral.connect(nodes["node3"]["endpoint"],
                                    token=nodes["node3"]["token"]) as c:
        state, status = await node3_view(c)
        assert state == "unclaimed", (
            f"node3 reads itself {state!r} after the restart; an expelled "
            "node stays 'unclaimed': 'claimed' means its contract came back, "
            "'fresh' that its ownership history was lost")
        assert isinstance(status, astral.errors.Denied), (
            f"node3's own gate answers swarm_status with {status!r}, not "
            "denied, though node3 has no contract")

        join = await refusal(c.user.join(id1))
        assert EXPELLED in str(join), (
            f"node1 did not refuse node3's user.join as expelled after the "
            f"restart: {join!r}")

    async with await astral.connect(nodes["node1"]["endpoint"],
                                    token=user_token) as c:
        readopt = await refusal(c.user.adopt(id3))
        assert EXPELLED in str(readopt), (
            f"node1 did not refuse to re-adopt node3 after the restart: "
            f"{readopt!r}")

    # --- after the attempts: nothing changed -------------------------------
    for name, token in (("node1", user_token), ("node2", nodes["node2"]["token"])):
        async with await astral.connect(nodes[name]["endpoint"],
                                        token=token) as c:
            roster = {str(m.identity) for m in await c.user.swarm_status()}
            assert roster == remaining, (
                f"after node3's attempts {name}'s roster is "
                f"{names(roster, by_id)}, not exactly node1+node2")
    async with await astral.connect(nodes["node3"]["endpoint"],
                                    token=nodes["node3"]["token"]) as c:
        state = (await c.user.info()).state
        assert state == "unclaimed", (
            f"node3 is {state!r} after its refused join, not 'unclaimed'")

    print("oracle: all three nodes restarted once; node1 and node2 hold "
          "exactly the User's ban on node3, list exactly node1+node2 as "
          "roster and as linked siblings; node3 is unclaimed with no "
          f"contract, node1 refuses its join and its re-adoption ({EXPELLED!r})")


asyncio.run(main())
