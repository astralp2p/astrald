#!/usr/bin/env python3
"""Driver: re-establish the swarm link over Tor after node2 leaves the LAN.

Env-blind: the endpoints come from session.json, so this is the same shape as
every node-env driver. The steps before it took node2 off the LAN, so the only
mutual transport left is Tor.

why this waits instead of trusting the call: astrald's Tor strategy signals
done after its quick timeout and keeps trying in the background for up to six
minutes more — "TorLinkStrategy tries to connect with retries. After
quickTimeout it signals Done but continues running in background", with a 60 s
SignalTimeout against a 360 s BackgroundTimeout (mod/nodes/src/loader.go).
`new_link` answering "link not produced" therefore means "not yet", not "no",
and a driver that took it as final failed a test the daemon was still busy
passing. The question this test asks is whether the swarm re-forms over Tor,
so it asks that: kick the attempt off, then watch the link table.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts

LINK_DEADLINE = 300.0     # inside the manifest's 600 s, outside astrald's 360 s
POLL = 5.0


async def tor_networks(c, peer: str) -> list:
    """Networks node1 currently holds a link to `peer` over."""
    links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        try:
            await c.nodes.new_link(n2["identity"], strategies="tor",
                                   experimental=True, timeout=LINK_DEADLINE)
        except astral.errors.RemoteError as e:
            # the strategy gave up early and is still working; say so and wait
            print(f"driver: new_link returned early ({e}); "
                  "the tor strategy keeps trying in the background")

        deadline = time.monotonic() + LINK_DEADLINE
        networks = await tor_networks(c, n2["identity"])
        while "tor" not in networks and time.monotonic() < deadline:
            await asyncio.sleep(POLL)
            networks = await tor_networks(c, n2["identity"])

        waited = LINK_DEADLINE - max(0.0, deadline - time.monotonic())

    write_facts({"tor_link_networks": networks})
    if "tor" not in networks:
        raise SystemExit(
            f"driver: no tor link to node2 after {waited:.0f}s; "
            f"node1 holds {networks or 'no links'} to it")
    print(f"driver: node1 linked to node2 over {networks} after {waited:.0f}s")


asyncio.run(main())
