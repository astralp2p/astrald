#!/usr/bin/env python3
"""Driver: a second User's swarm beside the first, one link between them.

adopt-node left User A's swarm: node1 and node2. This driver claims `tssb1`
for a second User B with a token-less bootstrap, adopts `tssb2` into B's swarm
as B, and links node2 to `tssb1` from node2's side. Then it offers each swarm's
node contracts to a node of the other swarm over that link, and the same
contracts to a node of their own swarm as a control. It records what each node
answered; the oracle judges, and polls the live swarms itself.

why a push: nothing in a healthy node sends a contract of one User to another
User's node, and an accepted foreign contract changes no sibling list (every
list filters on the active contract's issuer). The push is the only way to
make a node decide whether to take one, and the answer is the only place the
decision shows.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts

DEADLINE = 30.0


async def link(a: dict, b: dict) -> None:
    """a learns b's address and dials it, as itself; b learns a's too."""
    async with await astral.connect(a["endpoint"], token=a["token"]) as c:
        await c.nodes.add_endpoint(b["identity"], b["lan_endpoint"], experimental=True)
        await c.nodes.new_link(b["identity"], endpoint=b["lan_endpoint"],
                               strategies="basic", experimental=True,
                               timeout=60.0)
    async with await astral.connect(b["endpoint"], token=b["token"]) as c:
        await c.nodes.add_endpoint(a["identity"], a["lan_endpoint"], experimental=True)


async def push(pusher: dict, target: dict, contract) -> bool:
    """The pusher's node offers the contract to the target; whether it was accepted."""
    async with await astral.connect(pusher["endpoint"], token=pusher["token"]) as c:
        (accepted,) = await asyncio.wait_for(
            c.objects.push(contract, target=target["identity"]), DEADLINE)
    return accepted


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    b1, b2 = doc["nodes"]["tssb1"], doc["nodes"]["tssb2"]

    # User B: claimed on tssb1, then tssb2 adopted as B (adopt-node's steps)
    async with await astral.connect(b1["endpoint"]) as anon:
        boot = await anon.user.bootstrap(None)
    b_id, b_token = str(boot.identity), str(boot.token)

    await link(b1, b2)
    async with await astral.connect(b1["endpoint"], token=b_token) as c:
        b2_contract = await c.user.adopt(b2["identity"])

    # the one link between the swarms, dialled by node2
    await link(n2, b1)

    # node contracts as their issuers' nodes hold them: node2's for A (read
    # by node1, a linked sibling), tssb2's for B (what adopt returned)
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        a2_contract = await asyncio.wait_for(
            c.user.contract(target=n2["identity"]), DEADLINE)

    pushes = {
        # into the other swarm: each must be refused
        "a_contract_to_b_node": await push(n2, b1, a2_contract),
        "b_contract_to_a_node": await push(b1, n2, b2_contract),
        # into the own swarm: each must be taken, or a refusal says nothing
        "a_contract_to_a_node": await push(n1, n2, a2_contract),
        "b_contract_to_b_node": await push(b1, b2, b2_contract),
    }

    write_facts({"b_user_id": b_id, "b_user_token": b_token, "pushes": pushes})
    print(f"driver: swarm B ({b_id[:16]}…) holds tssb1 and tssb2; node2 linked "
          f"to tssb1; pushes {pushes}")


asyncio.run(main())
