#!/usr/bin/env python3
"""Driver: the first User tries to adopt acn-b2, a node User B owns.

node1 learns acn-b2's endpoint and links to it, as adopt-node does, so the
node can be reached and the answer is its own. As the first User on node1 the
driver then asks user.adopt for acn-b2. The outcome, the refusal or the
contract that came back, as text, is the `acn_adopt_outcome` fact; the oracle judges
it and the nodes' own state.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1, b2 = doc["nodes"]["node1"], doc["nodes"]["acn-b2"]
    user_token = doc["facts"]["user_token"]

    # nodes.* is Tier-3 gated in astral-py: experimental=True per call.
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.nodes.add_endpoint(b2["identity"], b2["lan_endpoint"],
                                   experimental=True)

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        await c.nodes.new_link(b2["identity"], endpoint=b2["lan_endpoint"],
                               strategies="basic", experimental=True,
                               timeout=60.0)
        try:
            signed = await c.user.adopt(b2["identity"], timeout=60.0)
        except AstralError as e:
            outcome = f"refused: {type(e).__name__}: {e}"
        else:
            outcome = f"answered: contract issued by {signed.contract.issuer}"

    write_facts({"acn_adopt_outcome": outcome})
    print(f"driver: the first User's adopt of acn-b2: {outcome}")


asyncio.run(main())
