#!/usr/bin/env python3
"""Driver: queries to the sibling's node identity over a live link, both ways,
across a new link and the close of the older one.

20 rounds, 0.5 s apart. Before each round the driver reads both nodes' links.
Each round sends five queries, each with a 5 s deadline:

- node1's node token: `node2:user.info`, `node2:objects.contains` of an
  absent object;
- the User's token on node1: `node2:user.info`;
- node2's node token: `node1:user.info`, `node1:objects.contains`.

Before round 10 the User on node1 opens a second link to node2's
`lan_endpoint`; before round 15 node1 closes every link to node2 it held
before that one. The driver records the `sibling_query_live_link` fact:
{rounds, opened, closed, links_end} and judges nothing.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral
from astral.errors import (QueryCanceled, QueryRejected, QueryTimeout,
                           RemoteError, RouteNotFound)
from astral.objectid import object_id_of_bytes

from lib import linkview
from lib.sessionio import load, write_facts

ROUNDS = 20
EVERY = 0.5
OPEN_AT = 10
CLOSE_AT = 15
# why: a query no node routes would hang for the client's 60 s; the oracle
# judges 2 s, so 5 s keeps a slow answer apart from a lost one
DEADLINE = 5.0
# why absent: objects.contains answers false for it, which is still an answer
ABSENT = str(object_id_of_bytes(b"sibling-query-live-link absent object"))


def outcome(e: Exception) -> str:
    """The routing outcome class of a failed query (astral-docs
    core-definitions/query.md, Routing outcomes)."""
    if isinstance(e, RouteNotFound):
        return "route_not_found"
    if isinstance(e, QueryRejected):
        return {7: "unavailable", 6: "timeout"}.get(e.code, f"rejected:{e.code}")
    if isinstance(e, (QueryTimeout, asyncio.TimeoutError)):
        return "timeout"
    if isinstance(e, QueryCanceled):
        return "canceled"
    if isinstance(e, RemoteError):
        return "remote_error"
    return "error"


def answer(op: str, v) -> dict:
    """What an answered query returned, as plain values."""
    if op == "user.info":
        return {"type": type(v).__name__, "node": str(v.node_id) if v.node_id else None}
    return {"type": type(v).__name__, "value": bool(v)}


async def ask(c, op: str, target: str) -> dict:
    """One query to target's op, timed; every outcome is recorded."""
    start = time.monotonic()
    try:
        if op == "user.info":
            call = c.user.info(target=target)
        else:
            call = c.objects.contains("local", ABSENT, target=target)
        r = {"outcome": "ok", "answer": answer(op, await asyncio.wait_for(call, DEADLINE))}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        r = {"outcome": outcome(e), "err": f"{type(e).__name__}: {e}"[:300]}
    r["ms"] = round((time.monotonic() - start) * 1000, 1)
    return r


async def round_links(c1, c2, id1: str, id2: str) -> dict:
    """Both ends' links to each other before a round."""
    rows1, rows2 = linkview.to(await linkview.links(c1), id2), linkview.to(await linkview.links(c2), id1)
    return {"node1": rows1, "node2": rows2, "both": bool(rows1) and bool(rows2),
            "problems": linkview.two_view(id1, rows1, id2, rows2)}


async def open_link(cu, node2: dict) -> dict:
    """The User on node1 opens a new link to node2's lan_endpoint."""
    try:
        info = await cu.nodes.new_link(node2["identity"], endpoint=node2["lan_endpoint"],
                                       experimental=True, timeout=30.0)
        return {"id": str(info.id), "network": str(info.network), "t": time.time()}
    except Exception as e:  # noqa: BLE001 — the oracle judges the outcome
        return {"error": f"{type(e).__name__}: {e}", "t": time.time()}


async def close_older(c1, id2: str, older: set) -> dict:
    """node1 closes every link to node2 it held before the new one."""
    present = linkview.ids(await linkview.links(c1), id2)
    out = {"older": sorted(older), "closed": [], "gone_before": sorted(older - present),
           "t": time.time()}
    for link_id in sorted(older & present):
        try:
            await c1.nodes.close_link(link_id, experimental=True)
            out["closed"].append(link_id)
        except Exception as e:  # noqa: BLE001 — the oracle judges the outcome
            out.setdefault("errors", []).append(f"{link_id}: {type(e).__name__}: {e}")
    return out


async def one_round(clients: dict, ids: dict) -> list:
    """The five queries of one round, in order."""
    c1, cu, c2 = clients["node1"], clients["user"], clients["node2"]
    plan = [("node1", c1, "user.info", "node2"), ("node1", c1, "objects.contains", "node2"),
            ("user", cu, "user.info", "node2"),
            ("node2", c2, "user.info", "node1"), ("node2", c2, "objects.contains", "node1")]
    return [{"asker": a, "op": op, "to": to, **await ask(c, op, ids[to])}
            for a, c, op, to in plan]


async def run(clients: dict, nodes: dict) -> dict:
    ids = {n: nodes[n]["identity"] for n in ("node1", "node2")}
    c1, c2 = clients["node1"], clients["node2"]
    out = {"rounds": [], "opened": None, "closed": None}
    older = linkview.ids(await linkview.links(c1), ids["node2"])
    for i in range(ROUNDS):
        if i == OPEN_AT:
            out["opened"] = await open_link(clients["user"], nodes["node2"])
        if i == CLOSE_AT:
            out["closed"] = await close_older(c1, ids["node2"], older)
        links = await round_links(c1, c2, ids["node1"], ids["node2"])
        out["rounds"].append({"i": i, "t": time.time(), "links": links,
                              "queries": await one_round(clients, ids)})
        await asyncio.sleep(EVERY)
    out["links_end"] = await round_links(c1, c2, ids["node1"], ids["node2"])
    return out


async def main():
    doc = load()
    nodes = doc["nodes"]
    async with AsyncExitStack() as stack:
        clients = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in ("node1", "node2")}
        clients["user"] = await stack.enter_async_context(
            await astral.connect(nodes["node1"]["endpoint"], token=doc["facts"]["user_token"]))
        facts = await run(clients, nodes)
    write_facts({"sibling_query_live_link": facts})
    qs = [q for r in facts["rounds"] for q in r["queries"]]
    ok = sum(1 for q in qs if q["outcome"] == "ok")
    print(f"driver: {len(qs)} sibling-targeted queries in {ROUNDS} rounds, {ok} ok; "
          f"new link {facts['opened']}; closed {facts['closed']}")


asyncio.run(main())
