#!/usr/bin/env python3
"""Driver: a fresh node asks node2, a Privileged member without the User key,
for a Privileged membership, is refused, then asks node1, which holds the key,
and is admitted once the User confirms the join there.

The driver acts and judges nothing. It records each answer, and what the
three nodes hold once the refusal has had time to leave debris, for the
oracle.

Only the latecomer and node1 learn each other's endpoint, and the latecomer
learns node2's. node2 is never told where the latecomer accepts links, so
whatever node2 later knows of the latecomer came from the swarm.

why privileged_member: only the user key signs a Privileged membership (D2),
so it is the join a carrier without that key cannot end; a Member join through
node2 needs no answer, since node2, a Privileged carrier, signs it in the
carrier session.

why the pause after the refusal: a refused join that still left a membership
would spread it through SwarmState asynchronously, so the snapshot is taken
after a pause, not at the instant of the refusal.
"""
import asyncio

import astral
from astral.errors import AstralError, RemoteError

from lib import jsonops
from lib import swarmstate as ss
from lib.sessionio import load, write_facts

DEADLINE = 30
SETTLE = 3.0


def connect(node: dict, token: str | None = "node"):
    return astral.connect(node["endpoint"],
                          token=node["token"] if token == "node" else token)


async def endpoint(owner: dict, peer: dict) -> None:
    """`owner` learns the address a peer dials for `peer`."""
    async with await connect(owner) as c:
        await c.nodes.add_endpoint(peer["identity"], peer["lan_endpoint"],
                                   experimental=True)


async def join(latecomer: dict, via: dict) -> dict:
    """Ask `via` for a Privileged membership as the latecomer; how it was
    answered."""
    async with await connect(latecomer) as c:
        try:
            docs = await ss.join(c, via["identity"], tier="privileged_member",
                                 timeout=DEADLINE)
        except (RemoteError, jsonops.OpError) as e:
            return {"refused": True, "error": type(e).__name__, "text": str(e)}
        except AstralError as e:
            return {"refused": True, "error": type(e).__name__, "text": str(e)}
    if not docs or docs[0]["Type"] != "mod.auth.signed_contract":
        text = docs[0]["Object"] if docs else "answered nothing"
        return {"refused": True, "error": "error_message", "text": str(text)}
    contract = docs[0]["Object"]["Contract"]
    return {"refused": False, "error": "", "text": "answered",
            "issuer": contract["Issuer"], "subject": contract["Subject"]}


async def confirm(node: dict, user_token: str, joiner: str) -> None:
    """As the User on node: confirm joiner's JoinRequest (user.confirm_join)."""
    async with await connect(node, user_token) as c:
        r = await ss.find(c, lambda r: r.type == ss.JOIN and r.names(joiner)
                          and r.status == "pending", timeout=DEADLINE)
        await ss.confirm_join(c, r.id)


async def snapshot(nodes: dict) -> dict:
    """The latecomer's state, and each member's roster, on their own tokens."""
    out = {"latecomer_state": "", "rosters": {}}
    async with await connect(nodes["jvm-latecomer"]) as c:
        out["latecomer_state"] = (await c.user.info()).state
    for name in ("node1", "node2"):
        async with await connect(nodes[name]) as c:
            out["rosters"][name] = sorted(
                str(m.identity) for m in await c.user.swarm_status()
                if m.identity is not None)
    return out


async def main():
    doc = load()
    nodes, user_token = doc["nodes"], doc["facts"]["user_token"]
    n1, n2, late = nodes["node1"], nodes["node2"], nodes["jvm-latecomer"]

    await endpoint(late, n1)
    await endpoint(late, n2)
    await endpoint(n1, late)

    via2 = await join(late, n2)
    print(f"driver: join via node2: {via2['text']}")
    await asyncio.sleep(SETTLE)
    after = await snapshot(nodes)

    via1, _ = await asyncio.gather(join(late, n1),
                                   confirm(n1, user_token, late["identity"]))
    print(f"driver: join via node1: {via1['text']}")

    write_facts({"join_via_node2": via2, "after_refusal": after,
                 "join_via_node1": via1})


asyncio.run(main())
