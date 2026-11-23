#!/usr/bin/env python3
"""Driver: relay-contract-race's matrix over kcp alone: four siblings joined
by kcp links only, no relay contract pushed or carried, 12 first calls.

lib/kcponly.py turns tcp dial and listen off on every node, gives each node
the others' kcp rows, closes every link that is not kcp and links each pair
over kcp. The driver then taps every node's log (lib/nodelog.py) and starts
two mesh apps on each node (lib/meshapps.py, `period=0`, no follow):

- a provider (`m14p-<node>`), which registers, serves `hello` and advertises
  `m14`; once all four are ready,
- a consumer (`m14c-<node>`), whose targets are the three remote providers;
  it calls each once.

It waits at most 60 s until every consumer finished a first call to each
remote provider, and stops the apps.

why no follow and two identities: a node pushes an app's relay contract to
every swarm member when the app follows with reach=swarm (astral-docs
protocols/services/ops/services.discover.md, Discovery in an app's name), and
a node's call pushes the caller's contract to the callee's node
(topics/app-routing.md, Sending to a node over its own link). A provider that
followed or called would hand its contract to the consumers' nodes, which
would then route without the pull. The manifest sets
`apphost.skip_sibling_push` on every node, and the links were up before the
apps registered, so no push and no first-link sync carries a provider's
contract either: each consumer node pulls it when it routes the first call.

The driver records the `apps_matrix_kcp` fact: {join, providers_ready,
complete, providers, consumers, links_end, logs, taps}, where providers and
consumers are the apps' last states by node name, and logs holds, per
consumer node and remote provider node, the node's asks for the provider
app's relay contracts and the contracts it pulled. It judges nothing.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import kcponly, linkview, meshapps
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

SERVICE = "m14"
PROVIDERS = "m14p"
CONSUMERS = "m14c"
NAMES = ["node1", "node2", "node3", "node4"]
# the log lines of mod/nodes/src/pull_relays.go, as relay-contract-race reads them
ASK = "asking linked siblings for the relay contracts of "
PULLED = "pulled the relay contract of "
READY = 30.0
FIRST_CALLS = 60.0
POLL = 0.5
# why: an ask logs its pull once the contract is indexed; the call has
# returned by then, the log line may still be on its way
SETTLE = 1.0


def first(consumers: dict, providers: dict, c: str, p: str) -> dict | None:
    """Consumer c's first finished call to the provider on p, or None."""
    peer = (consumers[c].get("providers") or {}).get(providers[p].get("app"))
    return (peer or {}).get("first")


def all_first(consumers: dict, providers: dict) -> bool:
    """Whether every consumer finished a first call to each remote provider."""
    if any(not s.get("app") for s in (*consumers.values(), *providers.values())):
        return False
    return all(first(consumers, providers, c, p) is not None for c, p in meshapps.pairs(NAMES))


def pulls(taps: dict, app: dict) -> dict:
    """{consumer node: {provider node: {asks, pulled}}} from the taps."""
    return {c: {p: {"asks": [e["text"] for e in taps[c].find(ASK + app[p])],
                    "pulled": [e["text"] for e in taps[c].find(PULLED + app[p])]}
                for p in NAMES if p != c}
            for c in NAMES}


async def until(done, timeout: float) -> bool:
    """Poll done() until it holds or timeout passes; whether it held."""
    deadline = time.monotonic() + timeout
    while not done():
        if time.monotonic() >= deadline:
            return False
        await asyncio.sleep(POLL)
    return True


async def start(nodes: dict, spawned: list) -> bool:
    """Start the providers, then, once all are ready, the consumers; whether
    the providers became ready. spawned gets each prefix started."""
    spawned.append(PROVIDERS)
    meshapps.spawn(PROVIDERS, nodes, NAMES, service=SERVICE, period=0, follow=False)
    ready = await until(lambda: all(meshapps.ready(s) for s in
                                    meshapps.states(PROVIDERS, NAMES).values()), READY)
    if not ready:
        return False
    app = meshapps.ids(meshapps.states(PROVIDERS, NAMES))
    targets = {c: [app[p] for p in NAMES if p != c] for c in NAMES}
    spawned.append(CONSUMERS)
    meshapps.spawn(CONSUMERS, nodes, NAMES, service=SERVICE, period=0, follow=False,
                   targets=targets)
    return True


async def run_apps(nodes: dict) -> dict:
    """The apps' run under the log taps."""
    async with AsyncExitStack() as stack:
        taps = {n: await stack.enter_async_context(LogTap(nodes[n])) for n in NAMES}
        spawned = []
        try:
            ready = await start(nodes, spawned)
            if ready:
                await until(lambda: all_first(meshapps.states(CONSUMERS, NAMES),
                                              meshapps.states(PROVIDERS, NAMES)), FIRST_CALLS)
                await asyncio.sleep(SETTLE)
        finally:
            for prefix in reversed(spawned):
                meshapps.stop(prefix, NAMES)
        providers = meshapps.states(PROVIDERS, NAMES)
        consumers = meshapps.states(CONSUMERS, NAMES)
        return {"providers_ready": ready, "providers": providers, "consumers": consumers,
                "complete": all_first(consumers, providers),
                "logs": pulls(taps, meshapps.ids(providers)),
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
    firsts = [first(facts["consumers"], facts["providers"], c, p) if facts["complete"] else None
              for c, p in meshapps.pairs(NAMES)]
    ok = sum(1 for f in firsts if f and f["outcome"] == "ok")
    print(f"driver: kcp join settled {facts['join']['settled']}; "
          f"providers ready {facts['providers_ready']}; {ok} of 12 first calls ok")


asyncio.run(main())
