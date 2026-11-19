#!/usr/bin/env python3
"""Driver: punch from behind a port-translating NAT to a public peer, then echo.

node1 sits behind enter-nat's translating NAT (UDP 40000+n leaves as
20000+n, replies only); node2 is public on the LAN. Signaling rides the tcp
link add-reflector made from node1 to node2.

The driver acts and judges nothing. It records:
- `punch`: one timed `nat.punch` from node1 to node2 and the hole it returned;
- `nat_link_networks`: node1's networks to node2 after
  `nodes.new_link -strategies nat`;
- `isolated`: the links closed so kcp is the pair's only link;
- `echo`: 60 s of round trips from node1 to node2 over that kcp link.
"""
import asyncio

import astral

from lib import natpunch
from lib.sessionio import load, write_facts

LINK_DEADLINE = 120.0
PUNCH_DEADLINE = 20.0
NAT_DEADLINE = 180.0
ECHO_SECONDS = 60.0
ECHO_EVERY = 0.25


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=token) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        nets = await natpunch.link_over(c1, n2["identity"], "basic", "tcp",
                                        LINK_DEADLINE)
        print(f"driver: signaling transport — {nets}")

        punch = await natpunch.punch(c1, n2["identity"], PUNCH_DEADLINE)
        print(f"driver: nat.punch in {punch['seconds']} s — "
              f"{punch['hole'] or punch['errors']}")

        networks = await natpunch.link_over(c1, n2["identity"], "nat", "kcp",
                                            NAT_DEADLINE)
        facts = {"punch": punch, "nat_link_networks": networks}
        if "kcp" in networks:
            facts["isolated"] = await natpunch.isolate_kcp(
                c1, n1["identity"], c2, n2["identity"])
            facts["echo"] = await natpunch.echo(c1, n2["identity"],
                                                ECHO_SECONDS, ECHO_EVERY)

    write_facts(facts)
    if "kcp" not in networks:
        raise SystemExit(f"driver: the punch produced no kcp link; node1 "
                         f"holds {networks or 'no links'} to node2")


asyncio.run(main())
