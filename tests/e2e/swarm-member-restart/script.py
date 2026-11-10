#!/usr/bin/env python3
"""Driver: record what the three-node swarm holds before node3 restarts.

The harness restarts node3 after this driver (test.toml `restart`). Nothing
is changed here: the driver waits until all three nodes agree on the full
roster, so the oracle's "the roster is back" compares against a roster that
was whole, and records for the oracle what each node held:

- `roster`: the three node identities, sorted;
- `contracts`: each node's active-contract object id, as the node's own
  `user.info` reports it;
- `restarts_before`: how many times node3 had been restarted so far.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts

CONVERGE = 30.0
POLL = 1.0


async def view(c, me: str) -> dict:
    members = await c.user.swarm_status()
    info = await c.user.info()
    return {
        "members": {str(m.identity) for m in members
                    if m.identity is not None},
        "linked": {str(m.identity) for m in members
                   if m.linked and m.identity is not None},
        "contract": str(info.contract_id),
        "state": info.state,
        "user_id": str(info.user_id),
        "me": me,
    }


async def main():
    doc = load()
    nodes = {n: doc["nodes"][n] for n in ("node1", "node2", "node3")}
    ids = {n: nodes[n]["identity"] for n in nodes}
    roster = sorted(ids.values())

    views, contracts = {}, {}
    for name, node in nodes.items():
        want_linked = set(roster) - {ids[name]}
        async with await astral.connect(node["endpoint"],
                                        token=node["token"]) as c:
            deadline = time.monotonic() + CONVERGE
            v = await view(c, ids[name])
            while ((v["members"] != set(roster) or v["linked"] != want_linked)
                   and time.monotonic() < deadline):
                await asyncio.sleep(POLL)
                v = await view(c, ids[name])
        assert v["members"] == set(roster), (
            f"{name} lists swarm members {sorted(v['members'])}, not the "
            f"three nodes, before the restart; the world is not whole")
        assert v["linked"] == want_linked, (
            f"{name} is linked to {sorted(v['linked'])}, not to {sorted(want_linked)}, "
            "before the restart")
        assert v["state"] == "claimed", f"{name} is {v['state']!r} before the restart"
        views[name] = v
        contracts[name] = v["contract"]

    write_facts({
        "restarts_before": nodes["node3"].get("restarts") or 0,
        "contracts": contracts,
        "roster": roster,
    })
    print("driver: three-node roster whole before the restart:",
          ", ".join(f"{n}={c[:8]}…" for n, c in contracts.items()))


asyncio.run(main())
