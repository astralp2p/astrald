#!/usr/bin/env python3
"""Driver: link node2 over loopback with explicit endpoints, admit it into the
User's swarm as a Privileged node, alias both.

loopback has no LAN discovery, so both nodes learn each other's endpoint
first. node2 then asks to join through node1, its carrier, with a
privileged_member JoinRequest (user.join). node1 holds the software User
key, so the key signs node2's PrivilegedMembership only after the User's own
action: the driver, as the User on node1, finds the request in
user.swarm_requests and confirms it with user.confirm_join (D2). node1 then
hands the membership to node2, which activates it.
"""
import asyncio
import time

import astral

from lib import jsonops
from lib.sessionio import load

DEADLINE = 60.0


def connect(node: dict, token: str):
    return astral.connect(node["endpoint"], token=token)


async def join(n2: dict, n1: dict) -> list:
    """node2's user.join through node1; every envelope it answered."""
    async with await connect(n2, n2["token"]) as c:
        return await jsonops.call(
            c, f"user.join?node={n1['identity']}&tier=privileged_member&out=json",
            timeout=DEADLINE)


async def confirm(n1: dict, user_token: str, joiner: str) -> None:
    """As the User on node1: wait for joiner's request, then confirm it."""
    deadline = time.monotonic() + DEADLINE
    async with await connect(n1, user_token) as c:
        while time.monotonic() < deadline:
            docs = await jsonops.stream(c, "user.swarm_requests?out=json")
            # why triples: the op sends a request's entry ID, its status and
            # the signed request, in that order.
            for i in range(0, len(docs) - 2, 3):
                req_id, req = docs[i]["Object"], docs[i + 2]
                if req["Type"] == "mod.user.signed_join_request" and joiner in str(req):
                    await jsonops.value(c, f"user.confirm_join?id={req_id}&out=json")
                    return
            await asyncio.sleep(0.5)
    raise AssertionError(f"no join request of {joiner} reached node1")


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_token = doc["facts"]["user_token"]
    # why: session.json carries the address a PEER dials. Composing
    # 127.0.0.1 from tcp_port works only where both nodes share a loopback,
    # so it is the one thing that stopped this driver being env-blind.
    ep1, ep2 = n1["lan_endpoint"], n2["lan_endpoint"]

    # nodes.* is Tier-3 gated in astral-py: experimental=True per call.
    async with await connect(n1, n1["token"]) as c:
        await c.nodes.add_endpoint(n2["identity"], ep2, experimental=True)
    async with await connect(n2, n2["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], ep1, experimental=True)

    # as the User on node1: an explicit link first. new_link is RR and can be
    # slow — the caller's own timeout bounds it, so set one explicitly.
    async with await connect(n1, user_token) as c:
        await c.nodes.new_link(n2["identity"], endpoint=ep2,
                               strategies="basic", experimental=True,
                               timeout=60.0)

    joined, _ = await asyncio.gather(join(n2, n1), confirm(n1, user_token, n2["identity"]))
    assert joined and joined[0]["Type"] == "mod.auth.signed_contract", (
        f"node2's join answered {joined}")

    async with await connect(n1, user_token) as c:
        await c.dir.set_alias(n1["identity"], "node1")
        await c.dir.set_alias(n2["identity"], "node2")
    async with await connect(n2, n2["token"]) as c:
        await c.dir.set_alias(n1["identity"], "node1")
        await c.dir.set_alias(n2["identity"], "node2")

    print("driver: node2 joined through node1 as privileged_member, aliases set")


asyncio.run(main())
