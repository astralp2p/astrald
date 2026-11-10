#!/usr/bin/env python3
"""Oracle: after node3 restarts, the 3-node swarm is whole again.

The runner restarts node3 between driver and oracle (test.toml `restart`),
and the session view counts the restart. Claims, each on the node's own token
(the question is what that node itself believes):

- node3 was restarted exactly once since the driver ran;
- node3 still holds its membership: `user.info` answers `claimed` under the
  same User with the same active contract object it held before;
- on every node, `user.swarm_status` lists exactly the three nodes (self
  included, no stranger, no duplicate) and `user.list_siblings` lists exactly
  the other two, which are linked, within a window — node3 re-forms its links
  and its siblings re-link it.

Exact sets throughout: a node that comes back with a short roster, an extra
member or no contract fails, not only an empty one.
"""
import asyncio
import time

import astral

from lib.sessionio import load

# why a window: node3's links died with its process; node1 and node2 re-link
# it through their maintain-link tasks (backoff from 1 s), and node3 links its
# siblings once it has read its stored contract.
CONVERGE = 60.0
POLL = 1.0


def names(ids, by_id) -> str:
    return "[" + ", ".join(by_id.get(i, i[:12] + "…") for i in ids) + "]"


async def observe(c) -> dict:
    members = await c.user.swarm_status()
    sibs = await c.user.list_siblings()
    return {
        "members": sorted(str(m.identity) for m in members),
        "linked": sorted(str(m.identity) for m in members if m.linked),
        "siblings": sorted(str(s) for s in sibs),
    }


async def main():
    doc = load()
    facts = doc["facts"]
    nodes = {n: doc["nodes"][n] for n in ("node1", "node2", "node3")}
    ids = {n: nodes[n]["identity"] for n in nodes}
    by_id = {v: k for k, v in ids.items()}
    roster = sorted(facts["roster"])
    assert roster == sorted(ids.values()), (
        f"the recorded roster {roster} is not the world's nodes")
    user_id = doc["facts"]["user_id"]

    restarts = (nodes["node3"].get("restarts") or 0) - facts["restarts_before"]
    assert restarts == 1, (
        f"node3 was restarted {restarts} times between driver and oracle, "
        "not once")

    for name, node in nodes.items():
        others = sorted(set(roster) - {ids[name]})
        async with await astral.connect(node["endpoint"],
                                        token=node["token"]) as c:
            info = await c.user.info()
            assert info.state == "claimed", (
                f"{name} is {info.state!r} after the restart, not claimed "
                "(membership not restored from storage)")
            assert str(info.user_id) == user_id, (
                f"{name} reports User {info.user_id!s}, not {user_id}")
            assert str(info.contract_id) == facts["contracts"][name], (
                f"{name} holds contract {info.contract_id!s}, not "
                f"{facts['contracts'][name]} from before the restart")

            deadline = time.monotonic() + CONVERGE
            got = await observe(c)
            while ((got["members"] != roster or got["linked"] != others
                    or got["siblings"] != others)
                   and time.monotonic() < deadline):
                await asyncio.sleep(POLL)
                got = await observe(c)
        assert got["members"] == roster, (
            f"{name}'s swarm_status lists {names(got['members'], by_id)}, not "
            f"exactly the three nodes, {CONVERGE:.0f} s after the restart "
            "(roster not rebuilt)")
        assert got["siblings"] == others, (
            f"{name}'s list_siblings is {names(got['siblings'], by_id)}, not "
            f"exactly {names(others, by_id)}, {CONVERGE:.0f} s after the "
            "restart (siblings not re-linked)")
        assert got["linked"] == others, (
            f"{name}'s swarm_status marks {names(got['linked'], by_id)} linked, "
            f"not exactly {names(others, by_id)}")

    print(f"oracle: node3 restarted once; all three nodes list exactly "
          f"{len(roster)} members and each links exactly the other two; "
          "contracts unchanged")


asyncio.run(main())
