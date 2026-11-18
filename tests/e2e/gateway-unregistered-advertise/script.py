#!/usr/bin/env python3
"""Driver: once the gateway is gone, node2 stops advertising itself through it.

gateway-relay left node2 registered with the reflector, and its oracle saw
node2 resolve `gw:<reflector>:<node2>` for itself. `stop-astrald reflector`
has stopped the gateway since. node2's idle connection pool to the gateway
fails, the pool ends, and the registration flag goes with it, so node2's own
resolver drops the gw row.

The driver polls node2's `nodes.resolve_endpoints localnode` until no gw row
through the reflector is left, and records how long that took.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts

DEADLINE = 300.0
INTERVAL = 3.0


async def gw_rows(c, gateway):
    """node2's own gw rows through the gateway, as `network:address`."""
    rows = await c.nodes.resolve_endpoints("localnode", experimental=True,
                                           timeout=30)
    return sorted(f"{r.endpoint.network()}:{r.endpoint.address()}"
                  for r in rows if r.endpoint.network() == "gw"
                  and r.endpoint.address().startswith(gateway + ":"))


async def main():
    doc = load()
    n2, refl = doc["nodes"]["node2"], doc["nodes"]["reflector"]
    loop = asyncio.get_running_loop()
    start = loop.time()
    facts = {"cleared": False, "cleared_after_s": None, "gw_rows": []}

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        while True:
            facts["gw_rows"] = await gw_rows(c, refl["identity"])
            if not facts["gw_rows"]:
                facts["cleared"] = True
                facts["cleared_after_s"] = round(loop.time() - start, 1)
                break
            if loop.time() - start >= DEADLINE:
                break
            await asyncio.sleep(INTERVAL)

    write_facts(facts)
    print(f"driver: node2's gw rows through the stopped gateway: "
          f"{facts['gw_rows'] or 'none'}, cleared after "
          f"{facts['cleared_after_s']} s")


asyncio.run(main())
