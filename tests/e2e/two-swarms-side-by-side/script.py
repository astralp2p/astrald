#!/usr/bin/env python3
"""Driver: a second User's swarm beside the first, one link between them.

adopt-node left User A's swarm: node1 and node2. This driver claims `tssb1`
for a second User B with a token-less bootstrap, admits `tssb2` into B's swarm
as a privileged_member through tssb1 (adopt-node's steps), and links node2 to
`tssb1` from node2's side. Then it offers each swarm's node membership to a
node of the other swarm over that link with `user.swarm_push`, and the same
membership to a node of its own swarm as a control. It records what each node
answered; the oracle judges, and polls the live swarms itself.

why an offered entry: since G7 a membership reaches another node only through
SwarmState, and nothing in a healthy node offers one swarm's entry to a node of
another. The offer is the only way to make a node decide whether to take one;
its answer and the node's SwarmState show the decision.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts
from lib import swarmstate as ss

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


def push(pusher: dict, target: dict, entry_id: str) -> str:
    """The pusher's node offers its stored entry entry_id to the target with
    user.swarm_push: "ack", or the error the target answered."""
    return ss.offer(pusher, entry_id, [target])[target["identity"]]


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    b1, b2 = doc["nodes"]["tssb1"], doc["nodes"]["tssb2"]

    # User B: claimed on tssb1, then tssb2 admitted as B (adopt-node's steps)
    async with await astral.connect(b1["endpoint"]) as anon:
        boot = await anon.user.bootstrap(None)
    b_id, b_token = str(boot.identity), str(boot.token)

    await link(b1, b2)
    # why privileged_member: a Member's user.info names its carrier, not the
    # User (astral-py reads the active contract's issuer), and the oracle reads
    # each node's User; the user key signs a Privileged membership itself
    await ss.admit(b1, b_token, b2, tier="privileged_member")
    b2_membership = ss.membership_id(b1, b2["identity"])

    # the one link between the swarms, dialled by node2
    await link(n2, b1)

    # the memberships as their swarms store them: node2's for A, tssb2's for B
    a2_membership = ss.membership_id(n1, n2["identity"])

    pushes = {
        # into the other swarm: each must be refused
        "a_contract_to_b_node": push(n2, b1, a2_membership),
        "b_contract_to_a_node": push(b1, n2, b2_membership),
        # into the own swarm: each must be taken, or a refusal says nothing
        "a_contract_to_a_node": push(n1, n2, a2_membership),
        "b_contract_to_b_node": push(b1, b2, b2_membership),
    }

    write_facts({"b_user_id": b_id, "b_user_token": b_token, "pushes": pushes,
                 "memberships": {"a": a2_membership, "b": b2_membership}})
    print(f"driver: swarm B ({b_id[:16]}…) holds tssb1 and tssb2; node2 linked "
          f"to tssb1; pushes {pushes}")


asyncio.run(main())
