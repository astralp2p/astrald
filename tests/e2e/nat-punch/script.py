#!/usr/bin/env python3
"""Driver: hole-punch two symmetric-NAT'd peers into a direct kcp link.

Port of the netsim punch-nat task's decisive step. The trigger is
`nodes.new_link -strategies nat`, which drives NATLinkStrategy end to end;
`nat.punch` registers a Hole only and yields no link.

Signaling rides the Tor link the steps established: a tcp-only Basic strategy
cannot form between two symmetric NATs, and the punch client sets no relay
hint, so Tor is the sole mutual transport.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=token) as c:
        # why: the punch needs a live tor link to signal over; form one first
        # rather than discovering its absence as a punch timeout.
        await c.nodes.new_link(n2["identity"], strategies="tor",
                               experimental=True, timeout=300.0)
        await c.nodes.new_link(n2["identity"], strategies="nat",
                               experimental=True, timeout=600.0)
        links = await c.nodes.links(experimental=True)

    networks = sorted({l.network for l in links
                       if str(l.remote_identity) == n2["identity"]})
    write_facts({"nat_link_networks": networks})
    print(f"driver: node1 -> node2 over {networks}")


asyncio.run(main())
