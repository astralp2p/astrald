#!/usr/bin/env python3
"""Driver: hole-punch two symmetric-NAT'd peers into a direct kcp link.

Port of the netsim punch-nat task's decisive step. The trigger is
`nodes.new_link -strategies nat`, which drives NATLinkStrategy end to end;
`nat.punch` registers a Hole only and yields no link.

Signaling rides the Tor link the steps established: a tcp-only Basic strategy
cannot form between two symmetric NATs, and the punch client sets no relay
hint, so Tor is the sole mutual transport.

why both steps wait rather than trust their call: astrald's Tor strategy
signals done after a 60 s quick timeout and keeps trying for up to 360 s more
(mod/nodes/src/loader.go), so "link not produced" means "not yet". The punch
then needs that Tor link to exist before it can signal over it, which is
exactly the thing a premature verdict would hide.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts

TOR_DEADLINE = 300.0
NAT_DEADLINE = 300.0
POLL = 5.0


async def networks_to(c, peer: str) -> list:
    links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


async def link_over(c, peer: str, strategy: str, want: str,
                    deadline_s: float) -> list:
    """Kick off `strategy` and wait until a `want` link to peer exists."""
    try:
        await c.nodes.new_link(peer, strategies=strategy,
                               experimental=True, timeout=deadline_s)
    except astral.errors.RemoteError as e:
        print(f"driver: new_link({strategy}) returned early ({e}); waiting")

    deadline = time.monotonic() + deadline_s
    nets = await networks_to(c, peer)
    while want not in nets and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        nets = await networks_to(c, peer)
    return nets


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=token) as c:
        # why: the punch needs a live tor link to signal over; form one first
        # rather than discovering its absence as a punch timeout.
        nets = await link_over(c, n2["identity"], "tor", "tor", TOR_DEADLINE)
        if "tor" not in nets:
            raise SystemExit(
                f"driver: no tor link to signal the punch over; node1 holds "
                f"{nets or 'no links'} to node2")
        print(f"driver: signaling transport up — {nets}")

        networks = await link_over(c, n2["identity"], "nat", "kcp",
                                   NAT_DEADLINE)

    write_facts({"nat_link_networks": networks})
    if "kcp" not in networks:
        raise SystemExit(
            f"driver: the punch produced no kcp link; node1 holds "
            f"{networks or 'no links'} to node2")
    print(f"driver: node1 -> node2 over {networks}")


asyncio.run(main())
