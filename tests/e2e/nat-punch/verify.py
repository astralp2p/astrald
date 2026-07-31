#!/usr/bin/env python3
"""Oracle: a direct kcp link exists on BOTH peers after the punch.

Ported from the netsim punch-nat verify.py. A punch that leaves only the
signaling link is a failed punch, so tor alone is not a pass, and a link one
side believes in is not a link.
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

    assert "kcp" in from1, (
        f"node1 holds no direct kcp link to node2: {from1} — the punch did "
        "not promote past the tor signaling link")
    assert "kcp" in from2, (
        f"node2 holds no direct kcp link to node1: {from2} — the punch "
        "succeeded on one end only")
    print(f"oracle: direct kcp link on both ends (node1 {from1}, node2 {from2})")


asyncio.run(main())
