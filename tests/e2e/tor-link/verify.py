#!/usr/bin/env python3
"""Oracle: both peers hold a live tor link to each other, and no LAN path.

Ported from the netsim link-over-tor verify.py. Asserted on both ends: a link
node1 believes in that node2 does not have is not a link.
"""
import asyncio

import astral

from lib.sessionio import load


async def networks_to(endpoint, token, peer):
    async with await astral.connect(endpoint, token=token) as c:
        links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    from1 = await networks_to(n1["endpoint"], doc["facts"]["user_token"],
                              n2["identity"])
    from2 = await networks_to(n2["endpoint"], n2["token"], n1["identity"])

    assert "tor" in from1, f"node1 holds no tor link to node2: {from1}"
    assert "tor" in from2, f"node2 holds no tor link to node1: {from2}"
    assert "tcp" not in from1, (
        f"node1 still holds a LAN link to node2 ({from1}) — leave-lan did not "
        "sever the direct path, so the tor link proves nothing")
    print(f"oracle: tor link on both ends (node1 {from1}, node2 {from2})")


asyncio.run(main())
