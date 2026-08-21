#!/usr/bin/env python3
"""Driver: the User permanently bans node2 from the swarm.

Port of the netsim expel-node flow. Irreversible — astrald has no op that
lifts a ban, which is why this test declares mutates = true and nothing
downstream may consume two-nodes after it.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        signed = await c.user.expel(n2["identity"])

    subject = str(signed.expulsion.subject)
    write_facts({"expelled_id": subject})
    print(f"driver: node2 {subject[:16]}… expelled from the swarm")


asyncio.run(main())
