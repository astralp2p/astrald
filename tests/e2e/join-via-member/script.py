#!/usr/bin/env python3
"""Driver: a fresh node asks node2, a member without the User key, for
membership, is refused, then asks node1, which holds the key, and is admitted.

The driver acts and judges nothing. It records each answer, and what the
three nodes hold once the refusal has had time to leave debris, for the
oracle.

Only the latecomer and node1 learn each other's endpoint, and the latecomer
learns node2's. node2 is never told where the latecomer accepts links, so
whatever node2 later knows of the latecomer came from the swarm.

why the pause after the refusal: a refused join that still indexed or pushed
a contract would do it asynchronously (PushToLocalSwarm runs in a goroutine),
so the snapshot is taken after a pause, not at the instant of the refusal.
"""
import asyncio

import astral
from astral.errors import AstralError, RemoteError

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
    """Ask `via` for membership as the latecomer; how it was answered."""
    async with await connect(latecomer) as c:
        try:
            signed = await c.user.join(via["identity"], timeout=DEADLINE)
        except RemoteError as e:
            return {"refused": True, "error": "RemoteError", "text": e.message}
        except AstralError as e:
            return {"refused": True, "error": type(e).__name__, "text": str(e)}
    return {"refused": False, "error": "", "text": "answered",
            "issuer": str(signed.contract.issuer),
            "subject": str(signed.contract.subject)}


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
    nodes = load()["nodes"]
    n1, n2, late = nodes["node1"], nodes["node2"], nodes["jvm-latecomer"]

    await endpoint(late, n1)
    await endpoint(late, n2)
    await endpoint(n1, late)

    via2 = await join(late, n2)
    print(f"driver: join via node2: {via2['text']}")
    await asyncio.sleep(SETTLE)
    after = await snapshot(nodes)

    via1 = await join(late, n1)
    print(f"driver: join via node1: {via1['text']}")

    write_facts({"join_via_node2": via2, "after_refusal": after,
                 "join_via_node1": via1})


asyncio.run(main())
