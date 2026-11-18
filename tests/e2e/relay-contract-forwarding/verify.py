#!/usr/bin/env python3
"""Oracle: node3 learned node2's app from node1 alone, then reached it via node2.

The driver switched dialing off on node2 and node3 before node1 adopted node3,
so node2 could never deliver its greeter's relay contract to node3 itself
(astral-docs topics/app-routing.md, Reach; astrald mod/user/src/sync.go
syncAppContracts). Every step must have held, and node3 still names node2 as
the greeter's host.
"""
import asyncio

import astral

from lib import jsonops
from lib.sessionio import load

STEPS = ["dialing_off_on_node2_and_node3", "node3_adopted_on_node1",
         "node3_knows_the_greeter_on_node2",
         "node3_unlinked_from_node2_when_it_knew",
         "visitor_on_node3_greeted_through_node2"]


async def main():
    doc = load()
    facts = doc["facts"].get("relay_contract_forwarding", {})
    missing = [s for s in STEPS if facts.get(s) is not True]
    assert not missing, f"steps that did not hold: {missing}; facts: {facts}"

    n2, n3 = doc["nodes"]["node2"], doc["nodes"]["node3"]
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c:
        docs = await jsonops.call(c, f"apphost.hosts?app={facts['greeter']}&out=json")
    hosts = [d["Object"] for d in docs if d["Type"] == "identity"]
    assert hosts == [n2["identity"]], (
        f"apphost.hosts on node3 named {hosts} for the greeter; want node2 "
        f"{n2['identity']} alone: {docs}")

    print("oracle: node3 knew node2's greeter "
          f"{facts['seconds_to_learn']:.1f} s after its adoption, with no link "
          "to node2, and its visitor was greeted through node2")


asyncio.run(main())
