#!/usr/bin/env python3
"""Driver: link node2 over loopback with explicit endpoints, adopt it, alias both.

netsim's version rode LAN discovery (nearby); loopback has none, so this is
the playbook's explicit-endpoint path: add_endpoint both ways, new_link with
a direct endpoint, then user.adopt as the User.
"""
import asyncio

import astral

from lib.sessionio import load


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_token = doc["facts"]["user_token"]
    # why: session.json carries the address a PEER dials. Composing
    # 127.0.0.1 from tcp_port works only where both nodes share a loopback,
    # so it is the one thing that stopped this driver being env-blind.
    ep1, ep2 = n1["lan_endpoint"], n2["lan_endpoint"]

    # each node learns the other's endpoint (link-back after adoption).
    # nodes.* is Tier-3 gated in astral-py: experimental=True per call.
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.nodes.add_endpoint(n2["identity"], ep2, experimental=True)
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], ep1, experimental=True)

    # as the User on node1: explicit link, then adopt. new_link is RR and can
    # be slow — the caller's own timeout bounds it, so set one explicitly.
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        await c.nodes.new_link(n2["identity"], endpoint=ep2,
                               strategies="basic", experimental=True,
                               timeout=60.0)
        await c.user.adopt(n2["identity"])
        await c.dir.set_alias(n1["identity"], "node1")
        await c.dir.set_alias(n2["identity"], "node2")

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        await c.dir.set_alias(n1["identity"], "node1")
        await c.dir.set_alias(n2["identity"], "node2")

    print("driver: node2 linked and adopted, aliases set on both nodes")


asyncio.run(main())
