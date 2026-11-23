#!/usr/bin/env python3
"""Driver: every node and every app of a four-node swarm queries every other
node and app, and a linked node of another User probes the swarm's apps.

1. `f2stranger` is claimed by a second User through a token-less
   `user.bootstrap` and links to node1 from its own side, as
   swarm-authz-matrix's stranger does.
2. A mesh app (lib/meshapps.py, service `f2`, `period=0`) starts on node1 to
   node4, each probing the other three node identities once with
   `apphost.whoami` (app→node) and calling each provider its reach=swarm
   follow reports once (app→app). One more starts on `f2stranger`; its
   follow can see no swarm offering. The driver waits at most 60 s until
   every swarm app finished each first call and probe.
3. node→node: each node's token queries `<other node>:user.info`.
4. node→app: each node's token calls `hello` on each remote swarm app.
5. The stranger's node token calls `hello` on each swarm app and
   `user.swarm_status` on node1; a fresh app on the stranger
   (`meshapp call -as-app`) calls `hello` on each swarm app.

Every query has a 15 s deadline. The driver stops the apps and records the
`everyone_reaches_everyone` fact: {nodes, apps, states, node_node, node_app,
stranger}. It judges nothing.
"""
import asyncio

import astral

from lib import meshapps
from lib.sessionio import load, write_facts

PREFIX = "f2"
NAMES = ["node1", "node2", "node3", "node4"]
STRANGER = "f2stranger"
# why: a query no node routes would hang; 15 s keeps a slow answer apart from
# a lost one, past the oracle's 10 s
DEADLINE = 15
FIRST_CALLS = 60.0
POLL = 0.5


async def claim_and_link(stranger: dict, node1: dict) -> None:
    """A second User claims the stranger, which then links to node1.

    why the stranger dials: node1 is given no endpoint of the stranger, so it
    holds no record it did not have before but the link.
    """
    async with await astral.connect(stranger["endpoint"]) as anon:
        await anon.user.bootstrap(None)
    async with await astral.connect(stranger["endpoint"], token=stranger["token"]) as c:
        await c.nodes.add_endpoint(node1["identity"], node1["lan_endpoint"], experimental=True)
        await c.nodes.new_link(node1["identity"], endpoint=node1["lan_endpoint"],
                               strategies="basic", experimental=True, timeout=60.0)


def settled(states: dict) -> bool:
    """Whether every swarm app finished a first call to each remote app and a
    first probe of each remote node."""
    swarm = {n: states[n] for n in NAMES}
    if any(not s.get("app") for s in swarm.values()):
        return False
    if any(f is None for f in meshapps.first_calls(swarm, NAMES).values()):
        return False
    return all(len([p for p in (s.get("probes") or {}).values() if p.get("first")]) == 3
               for s in swarm.values())


def queries(nodes: dict, pairs: list, op: str, to: dict) -> list:
    """One `meshapp call` per (caller node, callee) pair, with its node token."""
    return [{"from": c, "to": p, "op": op,
             **meshapps.call(nodes[c], target=to[p], op=op, timeout=DEADLINE)}
            for c, p in pairs]


def stranger_probes(nodes: dict, app: dict) -> dict:
    s = nodes[STRANGER]
    return {
        "node_app": [{"to": p, **meshapps.call(s, target=app[p], timeout=DEADLINE)}
                     for p in NAMES],
        "app_app": [{"to": p, **meshapps.call(s, target=app[p], as_app=True, timeout=DEADLINE)}
                    for p in NAMES],
        "swarm_status": meshapps.call(s, target=nodes["node1"]["identity"],
                                      op="user.swarm_status", timeout=DEADLINE),
    }


async def wait_settled() -> None:
    deadline = asyncio.get_running_loop().time() + FIRST_CALLS
    while not settled(meshapps.states(PREFIX, NAMES)):
        if asyncio.get_running_loop().time() >= deadline:
            return
        await asyncio.sleep(POLL)


async def main():
    nodes = load()["nodes"]
    ids = {n: nodes[n]["identity"] for n in [*NAMES, STRANGER]}
    await claim_and_link(nodes[STRANGER], nodes["node1"])
    probe = {n: [ids[p] for p in NAMES if p != n] for n in NAMES}
    meshapps.spawn(PREFIX, nodes, NAMES, service=PREFIX, period=0, probe_nodes=probe)
    meshapps.spawn(PREFIX, nodes, [STRANGER], service=PREFIX, period=0)
    everyone = [*NAMES, STRANGER]
    try:
        app = meshapps.ids(meshapps.wait_ready(PREFIX, everyone))
        await wait_settled()
        pairs = meshapps.pairs(NAMES)
        facts = {"nodes": ids, "apps": app,
                 "node_node": queries(nodes, pairs, "user.info", ids),
                 "node_app": queries(nodes, pairs, "hello", app),
                 "stranger": stranger_probes(nodes, app)}
    finally:
        meshapps.stop(PREFIX, everyone)
    facts["states"] = meshapps.states(PREFIX, everyone)
    write_facts({"everyone_reaches_everyone": facts})
    n = sum(1 for k in ("node_node", "node_app") for q in facts[k] if q["outcome"] == "ok")
    print(f"driver: {n} of 24 node-token queries ok; stranger probes "
          f"{[q['outcome'] for q in facts['stranger']['app_app']]}")


asyncio.run(main())
