#!/usr/bin/env python3
"""Driver: node3 joins the swarm of node1 and node2 as a Member.

node3 and node1 learn each other's endpoint. node3 asks to join through node1,
its carrier, with a member JoinRequest (user.join). The User, on node1, finds
the request in user.swarm_requests and approves it (user.answer_request), so
node1, a Privileged node, signs the Approval. node1 then signs node3's
membership and hands it over; node3 activates it. node2 and node3 then learn
each other's endpoint, so every pair of the three can link: the scenarios
after this one test authority, not how the swarm meshes.

Records the `member_join` fact: {request_id, status, membership_request,
membership}.
"""
import asyncio

import astral

from lib import swarmstate
from lib.sessionio import load, write_facts


def connect(node: dict, token: str):
    return astral.connect(node["endpoint"], token=token)


async def meet(a: dict, b: dict) -> None:
    """a and b learn each other's endpoint."""
    async with await connect(a, a["token"]) as c:
        await c.nodes.add_endpoint(b["identity"], b["lan_endpoint"], experimental=True)
    async with await connect(b, b["token"]) as c:
        await c.nodes.add_endpoint(a["identity"], a["lan_endpoint"], experimental=True)


async def main():
    doc = load()
    nodes = doc["nodes"]
    n1, n2, n3 = nodes["node1"], nodes["node2"], nodes["node3"]
    await meet(n1, n3)

    async with await connect(n3, n3["token"]) as c3, \
            await connect(n1, doc["facts"]["user_token"]) as cu:
        joined, req = await swarmstate.admit_member(c3, cu, n1["identity"], n3["identity"])
        approved = await swarmstate.find(cu, lambda r: r.id == req.id)

    assert joined and joined[0]["Type"] == "mod.auth.signed_contract", (
        f"node3's join answered {joined}")
    await meet(n2, n3)
    membership = joined[0]["Object"]["Contract"]
    write_facts({"member_join": {"request_id": req.id, "status": approved.status,
                                 "membership_request": membership.get("RequestID"),
                                 "membership": membership}})
    print(f"driver: node3 joined through node1 as member (request {req.id})")


asyncio.run(main())
