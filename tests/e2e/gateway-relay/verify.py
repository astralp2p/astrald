#!/usr/bin/env python3
"""Oracle: the relay carried it, and no direct path was left to carry it instead.

Three claims, and the middle one keeps the others honest. A gateway test that
only shows a node answering proves nothing on a LAN where every node can dial
every other — the question is always whether the traffic went the long way
because it had to.

So the verdict is the link's network, read live from node1 and filtered to
node2. A `gw` link is the relay. A `tcp` link to a NAT'd node would mean a
direct path survived and the world was never what the test claimed — and the
filter matters, because node1 holds an ordinary tcp link to the gateway itself
and an unfiltered check would trip over it.

Unregistering is not tested here. It is destructive and would have to run last,
which would either tear down the link before this oracle could read it or leave
the decisive claim resting on the driver's own account. A separate act, once
this one is green.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib.sessionio import load


async def networks_to(endpoint, token, peer):
    """The exonet networks of every link this node holds to that peer."""
    async with await astral.connect(endpoint, token=token) as c:
        links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    refl = doc["nodes"]["reflector"]
    facts = doc["facts"]

    faults = []
    if not facts["registered"]:
        faults.append(
            "node2 never appeared in the gateway's node_list — a node "
            "configured with a gateway did not register with it, so nothing "
            "downstream could have gone through the relay")
    if not facts["linked"]:
        faults.append(
            f"node1 never linked to node2 over {facts['endpoint']} — a node "
            "reachable only through a gateway stayed unreachable")

    to_node2 = await networks_to(n1["endpoint"], n1["token"], n2["identity"])
    if "gw" not in to_node2:
        faults.append(
            f"node1's links to node2 are {to_node2 or 'none'} — no gw link, so "
            "whatever reached it did not come through the gateway")
    if "tcp" in to_node2:
        faults.append(
            f"node1 holds a tcp link to a NAT'd node ({to_node2}) — a direct "
            "path survived and the relay was never the only way")

    # The living check: node2 answers for itself over that link.
    if "gw" in to_node2:
        try:
            async with await astral.connect(n1["endpoint"],
                                            token=n1["token"]) as c1:
                answered = str(await c1.apphost.whoami(
                    target=n2["identity"], timeout=60))
            if answered != n2["identity"]:
                faults.append(
                    f"a query routed to node2 was answered by "
                    f"{answered[:16]}…, not node2 ({n2['identity'][:16]}…)")
        except AstralError as e:
            faults.append(
                f"node2 did not answer a query over the gw link: "
                f"{type(e).__name__}: {e}")

    assert not faults, "; ".join(faults)

    to_gw = await networks_to(n1["endpoint"], n1["token"], refl["identity"])
    print(f"oracle: node2 is registered with the gateway and unreachable "
          f"directly; node1 reaches it over {to_node2} and answers as itself. "
          f"node1's link to the gateway is {to_gw}, which is the hop that "
          f"carried it")


asyncio.run(main())
