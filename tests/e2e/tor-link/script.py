#!/usr/bin/env python3
"""Driver: re-establish the swarm link over Tor after node2 leaves the LAN.

Env-blind: the endpoints come from session.json, so this is the same shape as
every node-env driver. The steps before it took node2 off the LAN, so the only
mutual transport left is Tor.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        await c.nodes.new_link(n2["identity"], strategies="tor",
                               experimental=True, timeout=300.0)
        links = await c.nodes.links(experimental=True)

    networks = sorted({l.network for l in links
                       if str(l.remote_identity) == n2["identity"]})
    write_facts({"tor_link_networks": networks})
    print(f"driver: node1 linked to node2 over {networks}")


asyncio.run(main())
