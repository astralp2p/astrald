#!/usr/bin/env python3
"""Driver: four mesh apps started together on four siblings find each other
and call each reported provider once, over tcp, then again over kcp alone.

Phase `tcp`: a mesh app (tests/e2e/internal/meshapp, lib/meshapps.py) starts
on each node in one pass, service `f1-tcp`, `period=0`: each app advertises
the service, follows it with reach=swarm and calls each provider the follow
reports exactly once. The driver waits at most 60 s until every app finished
a first call to each of the three remote providers, keeps the states and the
call logs, and stops the apps.

Phase `kcp`: lib/kcponly.py turns tcp dial and listen off on every node,
gives each node the others' kcp rows, closes every link that is not kcp and
links each pair over kcp. Then the four apps start again with service
`f1-kcp`: fresh app identities, so fresh contracts.

The driver records the `swarm_apps_talk` fact: {tcp, kcp_join, kcp}, each
phase {starts, complete, states, calls, links_start, links_end,
tcp_settings}. It judges nothing.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import kcponly, linkview, meshapps
from lib.sessionio import load, write_facts

NAMES = ["node1", "node2", "node3", "node4"]
# why: the oracle's discovery-to-first-call bound is 10 s per pair; 60 s
# leaves room for a slow follow and still ends a phase that never completes
FIRST_CALLS = 60.0
POLL = 0.5


def all_first(states: dict) -> bool:
    """Whether every app finished a first call to each remote provider."""
    if any(not s.get("app") for s in states.values()):
        return False
    return all(f is not None for f in meshapps.first_calls(states, NAMES).values())


async def links(clients: dict) -> dict:
    return {n: await linkview.links(c) for n, c in clients.items()}


async def phase(prefix: str, nodes: dict, clients: dict) -> dict:
    """One phase: the four apps start together and call each other once."""
    out = {"links_start": await links(clients),
           "tcp_settings": {n: await kcponly.tcp_settings(c) for n, c in clients.items()}}
    meshapps.spawn(prefix, nodes, NAMES, service=prefix, period=0)
    try:
        deadline = time.monotonic() + FIRST_CALLS
        while not all_first(meshapps.states(prefix, NAMES)) and time.monotonic() < deadline:
            await asyncio.sleep(POLL)
    finally:
        meshapps.stop(prefix, NAMES)
    out["states"] = meshapps.states(prefix, NAMES)
    out["calls"] = {n: meshapps.calls(prefix, n) for n in NAMES}
    out["complete"] = all_first(out["states"])
    out["starts"] = {n: s.get("started_at") for n, s in out["states"].items()}
    out["links_end"] = await links(clients)
    return out


async def main():
    nodes = load()["nodes"]
    facts = {}
    async with AsyncExitStack() as stack:
        clients = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in NAMES}
        facts["tcp"] = await phase("f1-tcp", nodes, clients)
        facts["kcp_join"] = await kcponly.apply(nodes, NAMES)
        facts["kcp"] = await phase("f1-kcp", nodes, clients)
    write_facts({"swarm_apps_talk": facts})
    for name in ("tcp", "kcp"):
        firsts = meshapps.first_calls(facts[name]["states"], NAMES).values()
        ok = sum(1 for f in firsts if f and f["outcome"] == "ok")
        print(f"driver: phase {name}: {ok} of 12 first calls ok")
    print(f"driver: kcp join settled {facts['kcp_join']['settled']}")


asyncio.run(main())
