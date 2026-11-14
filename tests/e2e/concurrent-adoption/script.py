#!/usr/bin/env python3
"""Driver: four Users race to adopt one fresh node, five times over.

Each `cadu` node is claimed by a User of its own with a token-less
`user.bootstrap`. Each round takes the next fresh `cadp` node (the prize):
every adopter node learns the prize's address and links to it, and the prize
learns each adopter's. Then every User, on its own node, calls `user.adopt` of
the prize at once, from connections opened before the start, so the four
queries leave within the same moment. The driver records each answer, with its
start and end time, as the `rounds` fact; the oracle judges and reads the
final state itself.

why a fresh prize per round: a claimed node stays claimed, so a race repeats
only on a node no round has touched.
"""
import asyncio
import contextlib
import time

import astral
from astral.objectid import object_id

from lib.sessionio import load, write_facts

ADOPTERS = ("cadu1", "cadu2", "cadu3", "cadu4")
PRIZES = ("cadp1", "cadp2", "cadp3", "cadp4", "cadp5")
ADOPT_TIMEOUT = 60.0


async def link(a: dict, b: dict) -> None:
    """a learns b's address and dials it, as itself; b learns a's too."""
    async with await astral.connect(a["endpoint"], token=a["token"]) as c:
        await c.nodes.add_endpoint(b["identity"], b["lan_endpoint"], experimental=True)
        await c.nodes.new_link(b["identity"], endpoint=b["lan_endpoint"],
                               strategies="basic", experimental=True,
                               timeout=60.0)
    async with await astral.connect(b["endpoint"], token=b["token"]) as c:
        await c.nodes.add_endpoint(a["identity"], a["lan_endpoint"], experimental=True)


async def claim(node: dict) -> dict:
    """A User of its own claims the node; its identity and token."""
    async with await astral.connect(node["endpoint"]) as anon:
        boot = await anon.user.bootstrap(None)
    return {"user_id": str(boot.identity), "user_token": str(boot.token)}


async def adopt(c, prize: str, t0: float) -> dict:
    """One User's adopt of the prize: the answer and when it started and ended."""
    start = time.monotonic() - t0
    try:
        signed = await c.user.adopt(prize, timeout=ADOPT_TIMEOUT)
        answer = {"outcome": "answered", "message": str(object_id(signed))}
    except astral.errors.RemoteError as e:
        answer = {"outcome": "refused", "message": e.message}
    except Exception as e:  # noqa: BLE001 — recorded, the oracle rejects it
        answer = {"outcome": "failed", "message": f"{type(e).__name__}: {e}"}
    answer["start_ms"] = round(start * 1000, 1)
    answer["end_ms"] = round((time.monotonic() - t0) * 1000, 1)
    return answer


async def race(nodes: dict, adopters: dict, prize_name: str) -> dict:
    """Every adopter links to the prize, then all adopt it at once."""
    prize = nodes[prize_name]
    await asyncio.gather(*(link(nodes[a], prize) for a in ADOPTERS))
    async with contextlib.AsyncExitStack() as stack:
        conns = [await stack.enter_async_context(
                     await astral.connect(nodes[a]["endpoint"],
                                          token=adopters[a]["user_token"]))
                 for a in ADOPTERS]
        t0 = time.monotonic()
        answers = await asyncio.gather(
            *(adopt(c, prize["identity"], t0) for c in conns))
    return {"prize": prize_name, "answers": dict(zip(ADOPTERS, answers))}


async def main():
    nodes = load()["nodes"]
    claims = await asyncio.gather(*(claim(nodes[a]) for a in ADOPTERS))
    adopters = dict(zip(ADOPTERS, claims))

    rounds = []
    for prize in PRIZES:
        r = await race(nodes, adopters, prize)
        rounds.append(r)
        for a, ans in r["answers"].items():
            print(f"driver: {prize} <- {a}: {ans['outcome']} {ans['message']} "
                  f"[{ans['start_ms']}..{ans['end_ms']} ms]")

    write_facts({"adopters": adopters, "rounds": rounds})


asyncio.run(main())
