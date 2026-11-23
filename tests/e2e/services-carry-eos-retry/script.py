#!/usr/bin/env python3
"""Driver: five times over, close the link that carries node1's swarm follow
to node2, after node2 delivered its initial outcome.

1. Spawns the `m12` mesh apps (`lib/meshapps.py`, period 0: one call each):
   the provider on node2, the follower on node1. node1 carries the
   follower's `reach=swarm` follow to node2 as a `services.discover` session.
2. The User opens five more links node1 -> node2 at node2's lan_endpoint, so
   a closed link leaves others and no new link wakes the carry.
3. Waits until the follower lists the provider. Then, per cycle: finds
   node1's outbound session to node2 that carries `services.discover`
   (`nodes.sessions`), closes its link (`nodes.close_link`), waits up to 30 s
   until the follower recorded the provider's removal and then its restore,
   lists node1's links to node2 before and after, and pauses 2 s.

The driver judges nothing and records the `carry_retry` fact: {provider,
follower, links_initial, cycles: [{closed_at, closed, sessions, links_before,
links_after, removal, restored_at}], error}.
"""
import asyncio
import time

import astral

from lib import apps, linkview, meshapps
from lib.sessionio import load, write_facts

PREFIX = "m12"
EXTRA_LINKS = 5
CYCLES = 5
WAIT = 30.0
PAUSE = 2.0
POLL = 0.1


def provider_record(follower: str, provider_app: str) -> dict:
    st = meshapps.states(PREFIX, [follower])[follower]
    return (st.get("providers") or {}).get(provider_app) or {}


async def until(pred, timeout: float):
    """The first truthy pred() within timeout, else None."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if got := pred():
            return got
        await asyncio.sleep(POLL)
    return None


async def carrying(c, id2: str) -> list:
    """node1's outbound sessions to node2 that carry services.discover."""
    rows = linkview.carrying(await linkview.sessions(c), "services.discover")
    return [r for r in rows if r["outbound"] and r["remote"] == id2]


async def cycle(c, id2: str, provider_app: str) -> dict:
    out = {"sessions": await until_sessions(c, id2)}
    out["links_before"] = sorted(linkview.ids(await linkview.links(c), id2))
    out["closed"] = sorted({r["link_id"] for r in out["sessions"]})
    for link_id in out["closed"]:
        await c.nodes.close_link(link_id, experimental=True)
    out["closed_at"] = closed_at = time.time()

    def restored():
        rec = provider_record("node1", provider_app)
        gone = [r for r in rec.get("removed") or [] if r["t"] >= closed_at - 0.5]
        back = [t for t in rec.get("restored") or [] if gone and t >= gone[0]["t"]]
        return (gone[0], back[0]) if back else None

    got = await until(restored, WAIT)
    out["removal"], out["restored_at"] = got if got else (None, None)
    out["links_after"] = sorted(linkview.ids(await linkview.links(c), id2))
    return out


async def until_sessions(c, id2: str) -> list:
    deadline = time.monotonic() + WAIT
    while time.monotonic() < deadline:
        if rows := await carrying(c, id2):
            return rows
        await asyncio.sleep(POLL)
    raise RuntimeError(f"node1 holds no outbound services.discover session to node2 within {WAIT:.0f} s")


async def flow(doc: dict, facts: dict) -> None:
    nodes = doc["nodes"]
    n2, id2 = nodes["node2"], nodes["node2"]["identity"]
    meshapps.spawn(PREFIX, nodes, ["node2", "node1"], service=PREFIX, period=0)
    states = meshapps.wait_ready(PREFIX, ["node2", "node1"])
    facts["provider"], facts["follower"] = states["node2"]["app"], states["node1"]["app"]
    async with await astral.connect(nodes["node1"]["endpoint"],
                                    token=doc["facts"]["user_token"]) as cu:
        for _ in range(EXTRA_LINKS):
            await cu.nodes.new_link(id2, endpoint=n2["lan_endpoint"], experimental=True,
                                    timeout=60.0)
    async with await astral.connect(nodes["node1"]["endpoint"], token=nodes["node1"]["token"]) as c:
        facts["links_initial"] = sorted(linkview.ids(await linkview.links(c), id2))
        listed = await until(lambda: provider_record("node1", facts["provider"]).get("listed"), WAIT)
        if not listed:
            raise RuntimeError(f"the follower did not list the provider within {WAIT:.0f} s")
        for _ in range(CYCLES):
            facts["cycles"].append(await cycle(c, id2, facts["provider"]))
            await asyncio.sleep(PAUSE)


async def main():
    doc = load()
    facts = {"cycles": [], "error": None}
    try:
        await flow(doc, facts)
    except Exception as e:  # noqa: BLE001 — the oracle reports how far the flow got
        facts["error"] = f"{type(e).__name__}: {e}"
    try:
        meshapps.stop(PREFIX, ["node2", "node1"])
    except apps.AppError:
        pass  # note: the flow failed before it spawned both apps; the runner reaps the rest
    write_facts({"carry_retry": facts})
    print(f"driver: {len(facts['cycles'])} cycles; error {facts['error']}")


asyncio.run(main())
