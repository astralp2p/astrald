#!/usr/bin/env python3
"""Oracle: a node advertises gw:G:self only while its registration with G is up.

gateway-relay's oracle saw node2 resolve `gw:<reflector>:<node2>` while it was
registered. With the reflector stopped, node2's own resolver must not list that
row any more: a sibling that pulled it would store a route that cannot work.

The verdict is read live from node2, not taken from the driver's account.
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n2, refl = doc["nodes"]["node2"], doc["nodes"]["reflector"]
    facts = doc["facts"]

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        rows = await c.nodes.resolve_endpoints("localnode", experimental=True,
                                               timeout=30)
    gw = sorted(f"{r.endpoint.network()}:{r.endpoint.address()}"
                for r in rows if r.endpoint.network() == "gw")

    faults = []
    if not facts["cleared"]:
        faults.append(
            f"node2 still resolved {facts['gw_rows']} for itself when the "
            "driver gave up — the registration flag outlived the gateway")
    through = [r for r in gw if r.startswith(f"gw:{refl['identity']}:")]
    if through:
        faults.append(
            f"node2 resolves {through} for itself with the gateway stopped")

    assert not faults, "; ".join(faults)
    print(f"oracle: node2 dropped its gw row {facts['cleared_after_s']} s into "
          f"the driver, with the gateway stopped; its gw rows now: "
          f"{gw or 'none'}")


asyncio.run(main())
