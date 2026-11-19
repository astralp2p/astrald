#!/usr/bin/env python3
"""Driver: leave node1 and node2 joined by kcp alone, then time a dead kcp dial.

Steps, before the runner restarts node2:

- each node learns the other's kcp rows: the kcp endpoints the other resolves
  for itself, and its kcp port at its `lan_endpoint` address;
- tcp dial and tcp listen go off through the tree on both nodes, so the
  setting survives node2's restart, and every non-kcp link between the pair
  is closed;
- node1 links to node2 at node2's kcp row (`nodes.new_link` as the User);
- both ends' links to each other settle on kcp alone;
- node1 dials node2 at a kcp address with nothing behind it, timed on the
  host's clock.

The driver judges nothing. It records the `kcp_restart_relink` fact.
"""
import asyncio
import time

import astral
from astral.primitives import Bool

from lib import linkview
from lib.sessionio import load, write_facts

# why under 1024: no unprivileged process binds it, so nothing answers there
DEAD_PORT = 919
# why: the dead dial's own budget; without a kcp bound it waits for this
DEAD_BUDGET = 90.0
TCP_SETTINGS = ("/mod/tcp/settings/dial", "/mod/tcp/settings/listen")


def lan_ip(node: dict) -> str:
    """The address of node's `lan_endpoint`, `tcp:<ip>:<port>`."""
    return node["lan_endpoint"].split(":")[1]


async def kcp_rows(c, node: dict) -> list:
    """The kcp rows a peer dials to reach the node behind c, the row at its
    `lan_endpoint` address first.

    why the lan_endpoint address: the node advertises no loopback address
    (mod/ip/src/module.go LocalIPs), and a local world's peers meet there.
    """
    own = [e.endpoint for e in await c.nodes.resolve_endpoints("localnode", experimental=True)]
    kcp = [e for e in own if callable(getattr(e, "network", None)) and e.network() == "kcp"]
    first = [f"kcp:{lan_ip(node)}:{p}" for p in sorted({e.port for e in kcp})]
    rest = sorted({f"kcp:{e.address()}" for e in kcp} - set(first))
    return first + rest


async def close_non_kcp(c, peer: str) -> list:
    """Close every link to peer that is not kcp; the ids closed."""
    closed = []
    for r in linkview.to(await linkview.links(c), peer):
        if r["network"] != "kcp":
            await c.nodes.close_link(r["id"], experimental=True)
            closed.append(f"{r['network']}:{r['id']}")
    return closed


async def dial(cu, peer: str, endpoint: str, timeout: float) -> dict:
    """One `nodes.new_link` to endpoint: its link or its error, timed."""
    start = time.monotonic()
    try:
        info = await cu.nodes.new_link(peer, endpoint=endpoint,
                                       experimental=True, timeout=timeout)
        r = {"id": str(info.id), "network": str(info.network)}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        r = {"error": f"{type(e).__name__}: {e}"}
    r["elapsed"] = round(time.monotonic() - start, 3)
    return r


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    facts = {"node2_restarts_before": n2.get("restarts") or 0}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2, \
            await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        rows1, rows2 = await kcp_rows(c1, n1), await kcp_rows(c2, n2)
        for r in rows2:
            await c1.nodes.add_endpoint(id2, r, experimental=True)
        for r in rows1:
            await c2.nodes.add_endpoint(id1, r, experimental=True)
        facts["rows"] = {"node1": rows1, "node2": rows2}

        for c in (c1, c2):
            for path in TCP_SETTINGS:
                await c.tree.set(path, Bool(False))
        facts["closed"] = await close_non_kcp(c1, id2) + await close_non_kcp(c2, id1)

        facts["kcp_link"] = await dial(cu, id2, rows2[0], 30.0) if rows2 else {"error": "node2 resolved no kcp row"}
        print(f"driver: kcp link {facts['kcp_link']}")

        async def both():
            a = linkview.to(await linkview.links(c1), id2)
            b = linkview.to(await linkview.links(c2), id1)
            return sorted((r["id"], r["network"]) for r in a), sorted((r["id"], r["network"]) for r in b)

        (view1, view2), settled = await linkview.settle(
            both, timeout=60, want=lambda v: v[0] and v[1] and all(n == "kcp" for _, n in v[0] + v[1]))
        facts["settled"] = settled
        facts["before"] = {"node1": view1, "node2": view2}
        print(f"driver: settled={settled} node1 {view1} node2 {view2}")

        facts["dead"] = await dial(cu, id2, f"kcp:{lan_ip(n2)}:{DEAD_PORT}", DEAD_BUDGET)
        facts["dead"]["budget"] = DEAD_BUDGET
        print(f"driver: dead kcp dial {facts['dead']}")

    write_facts({"kcp_restart_relink": facts})


asyncio.run(main())
