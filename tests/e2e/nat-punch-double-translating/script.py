#!/usr/bin/env python3
"""Driver: two nodes behind port-translating, port-restricted NATs punch, then echo.

Both nodes sit behind enter-nat's translating NAT (UDP 40000+n leaves as
20000+n, replies only), so neither reaches the other before the punch.
Signaling rides a Tor link. Each side reflects its punch socket off the
public `reflector` VM (nat.reflect_udp) and advertises the port the NAT
maps it to; Step 1's any-port receive alone cannot pass a port-restricted
filter on both sides.

The driver acts and judges nothing. It records:
- `punch`: one timed `nat.punch` from node1 to node2 and the hole it returned;
- `nat_link_networks`: node1's networks to node2 after
  `nodes.new_link -strategies nat`;
- `isolated`: the links closed so kcp is the pair's only link;
- `echo`: 60 s of round trips from node1 to node2 over that kcp link.

why the Tor wait (as in nat-punch): astrald's Tor strategy signals done
after a 60 s quick timeout and keeps trying, so the driver waits for the
link instead of trusting the call.
"""
import asyncio

import astral

from lib import natpunch
from lib.sessionio import load, write_facts

TOR_DEADLINE = 300.0
PUNCH_DEADLINE = 20.0
NAT_DEADLINE = 300.0
ECHO_SECONDS = 60.0
ECHO_EVERY = 0.25


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=token) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        nets = await natpunch.link_over(c1, n2["identity"], "tor", "tor",
                                        TOR_DEADLINE, poll_s=5.0)
        if "tor" not in nets:
            raise SystemExit(
                f"driver: no tor link to signal the punch over; node1 holds "
                f"{nets or 'no links'} to node2")
        print(f"driver: signaling transport up — {nets}")

        punch = await natpunch.punch(c1, n2["identity"], PUNCH_DEADLINE)
        print(f"driver: nat.punch in {punch['seconds']} s — "
              f"{punch['hole'] or punch['errors']}")

        networks = await natpunch.link_over(c1, n2["identity"], "nat", "kcp",
                                            NAT_DEADLINE, poll_s=5.0)
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
