#!/usr/bin/env python3
"""Driver: reach a node nobody can dial, through a gateway that can.

The lab's third VM is the gateway. node2 is behind NAT — it can open a
connection outwards and nothing can open one to it — so the only way node1
reaches it is the relay.

`configure-gateway` has already enabled the gateway on the reflector and
pointed node2 at it. Registration is astrald's own doing from there, through
MaintainGatewayConnectionsTask, so the driver waits for it rather than
performing it: a test that registered node2 by hand would be proving that the
op works, not that a configured node arrives.

node1 is then told one thing — a `gw:<gateway>:<target>` endpoint for node2 —
and asked to dial exactly that. That is the whole peer side, because there is
no direct address to give it.

The link is asked for through the SDK's own binding rather than a hand-built
query string. `nodes.new_link` takes `target`, not `id`, and a query missing a
required argument is rejected in microseconds by the argument decoder — which
reads exactly like a node refusing to link and cost a full netsim run to tell
apart. nat-punch's shape is the one to copy: kick the link off, then poll the
link list for the network wanted, because the node accepts the query before
the link exists.

The world is left standing. Every claim worth making is about the link that
results, and the oracle has to be able to see it.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib.sessionio import load, write_facts

REGISTER_DEADLINE = 180.0
LINK_DEADLINE = 120.0
INTERVAL = 3.0


async def poll(fn, deadline):
    loop = asyncio.get_running_loop()
    end = loop.time() + deadline
    while True:
        if await fn():
            return True
        if loop.time() >= end:
            return False
        await asyncio.sleep(INTERVAL)


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    refl = doc["nodes"]["reflector"]
    endpoint = f"gw:{refl['identity']}:{n2['identity']}"
    facts = {
        "gateway_identity": refl["identity"],
        "target_identity": n2["identity"],
        "endpoint": endpoint,
    }

    async with await astral.connect(refl["endpoint"], token=refl["token"]) as gw:

        async def listed():
            try:
                seen = [str(o) for o in await gw.call("gateway.node_list",
                                                      timeout=20)]
            except AstralError:
                return False
            return n2["identity"] in seen

        facts["registered"] = await poll(listed, REGISTER_DEADLINE)

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1:
        await c1.nodes.add_endpoint(n2["identity"], endpoint,
                                    experimental=True, timeout=30)

        async def networks_to_target():
            links = await c1.nodes.links(experimental=True)
            return sorted({l.network for l in links
                           if str(l.remote_identity) == n2["identity"]})

        try:
            await c1.nodes.new_link(n2["identity"], endpoint=endpoint,
                                    experimental=True, timeout=LINK_DEADLINE)
        except AstralError as e:
            print(f"driver: new_link returned early ({e}); waiting")

        async def has_gw():
            return "gw" in await networks_to_target()

        facts["linked"] = await poll(has_gw, LINK_DEADLINE)
        facts["networks_to_target"] = await networks_to_target()

    write_facts(facts)
    print(f"driver: node2 registered with the gateway: {facts['registered']}; "
          f"node1 links to node2: {facts['networks_to_target']}")


asyncio.run(main())
