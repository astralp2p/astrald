#!/usr/bin/env python3
"""Driver: relay-contract-race's matrix over kcp alone: four siblings joined
by kcp links only, no relay contract pushed, 12 first calls.

lib/kcponly.py turns tcp dial and listen off on every node, gives each node
the others' kcp rows, closes every link that is not kcp and links each pair
over kcp. The driver then taps every node's log (lib/nodelog.py) and starts a
mesh app on each node in one pass (lib/meshapps.py, service `m14`,
`period=0`): each app advertises, follows the service with reach=swarm and
calls each reported provider once. It waits at most 60 s until every app
finished a first call to each remote provider, and stops the apps.

The manifest sets `apphost.skip_sibling_push` on every node, and the links
were up before the apps registered, so no push and no first-link sync carries
an m14 contract: each consumer node pulls it when it routes the first call.

The driver records the `apps_matrix_kcp` fact: {join, complete, states,
links_end, logs, taps}, where logs holds, per consumer node and remote
provider node, the node's asks for the provider app's relay contracts and the
contracts it pulled. It judges nothing.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import kcponly, linkview, meshapps
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

PREFIX = "m14"
NAMES = ["node1", "node2", "node3", "node4"]
# the log lines of mod/nodes/src/pull_relays.go, as relay-contract-race reads them
ASK = "asking linked siblings for the relay contracts of "
PULLED = "pulled the relay contract of "
FIRST_CALLS = 60.0
POLL = 0.5
# why: an ask logs its pull once the contract is indexed; the call has
# returned by then, the log line may still be on its way
SETTLE = 1.0


def all_first(states: dict) -> bool:
    """Whether every app finished a first call to each remote provider."""
    if any(not s.get("app") for s in states.values()):
        return False
    return all(f is not None for f in meshapps.first_calls(states, NAMES).values())


def pulls(taps: dict, app: dict) -> dict:
    """{consumer node: {provider node: {asks, pulled}}} from the taps."""
    return {c: {p: {"asks": [e["text"] for e in taps[c].find(ASK + app[p])],
                    "pulled": [e["text"] for e in taps[c].find(PULLED + app[p])]}
                for p in NAMES if p != c}
            for c in NAMES}


async def run_apps(nodes: dict) -> dict:
    """The apps' run under the log taps."""
    async with AsyncExitStack() as stack:
        taps = {n: await stack.enter_async_context(LogTap(nodes[n])) for n in NAMES}
        meshapps.spawn(PREFIX, nodes, NAMES, service=PREFIX, period=0)
        try:
            deadline = time.monotonic() + FIRST_CALLS
            while not all_first(meshapps.states(PREFIX, NAMES)) and time.monotonic() < deadline:
                await asyncio.sleep(POLL)
            await asyncio.sleep(SETTLE)
        finally:
            meshapps.stop(PREFIX, NAMES)
        states = meshapps.states(PREFIX, NAMES)
        return {"states": states, "complete": all_first(states),
                "logs": pulls(taps, meshapps.ids(states)),
                "taps": {n: taps[n].state() for n in NAMES}}


async def links_now(nodes: dict) -> dict:
    out = {}
    for n in NAMES:
        async with await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]) as c:
            out[n] = await linkview.links(c)
    return out


async def main():
    nodes = load()["nodes"]
    facts = {"join": await kcponly.apply(nodes, NAMES)}
    facts.update(await run_apps(nodes))
    facts["links_end"] = await links_now(nodes)
    write_facts({"apps_matrix_kcp": facts})
    firsts = meshapps.first_calls(facts["states"], NAMES).values()
    ok = sum(1 for f in firsts if f and f["outcome"] == "ok")
    print(f"driver: kcp join settled {facts['join']['settled']}; {ok} of 12 first calls ok")


asyncio.run(main())
