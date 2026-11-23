#!/usr/bin/env python3
"""Driver: three fresh nodes of one User form one swarm across a LAN, a second subnet and a container bridge.

node1 and node2 share the 10.77 LAN, and node2 holds Docker's default bridge
address (add-bridge). move-subnet moves the reflector to 10.78.0.<octet>, a
subnet no other node holds, on the same segment.

1. Waits until the reflector holds its 10.78.0 address.
2. Claims node1 with `user.bootstrap` and generated words, then node2 and
   the reflector with the same words (as nearby-stealth-meet-ports). node2's
   claim starts the clock.
3. Polls node1's and node2's links every POLL s, at most LAN_WAIT s from
   node2's claim, until they share a link. Nothing gives either an endpoint.
4. On the reflector, as the User: `nodes.add_endpoint(node1,
   <node1's LAN endpoint>)` and `user.introduce(node1)`; polls at most
   INTRODUCE_WAIT s from the answer until the reflector shares a link with
   node1 and with node2. Nothing gives the reflector node2's endpoint.
5. Reads, on every node, the endpoints it resolves for each of the three,
   its links with both ends, and `user.swarm_status` until it lists the
   three with the other two linked, at most STATUS_WAIT s.

The driver taps every node's log for the nearby contact lines, so the
oracle can tell how each pair met. The reflector may hear node1 and node2
through nearby across the subnets; then it meets them before the
introduction, and the record shows it.

why the User's endpoint and introduction: a node that shares no local
network with a member joins through `user.introduce` after one member
endpoint is added (astral-docs topics/user-and-swarm.md, Replacing a node).

why jsonops: astral-py has no binding for user.bootstrap or user.introduce.

The driver acts and judges nothing. It records the `forms_and_finds` fact,
times in seconds since node2's claim, on the host's clock.
"""
import asyncio
import time
from contextlib import AsyncExitStack
from pathlib import Path

import astral
from astral.errors import AstralError

from lib import addrmove, jsonops
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

ROLES = ("node1", "node2", "reflector")
POLL = 2.0
# why: past the move's --in, the op's verifier and the session open
MOVE_MARGIN = 90.0
# why: two nearby status intervals of at most 95 s and margin, as
# nearby-stealth-meet-ports; the oracle holds the link to 200 s
LAN_WAIT = 240.0
INTRODUCE_WAIT = 90.0
STATUS_WAIT = 30.0
# why: the introduce push waits at most 30 s (astral-docs protocols/user/ops/user.introduce.md)
INTRODUCE_TIMEOUT = 45
# the user module's nearby contact lines (mod/user/src/peer_seen.go)
NEEDLES = ("nearby peer", "is nearby at")


def text(e) -> str | None:
    return None if e is None else f"{e.network()}:{e.address()}"


def session(node: dict, token: str | None = None):
    return astral.connect(node["endpoint"], token=token or node["token"])


async def links_of(node: dict) -> list:
    """node's links, with both endpoints."""
    async with await session(node) as c:
        infos = await c.nodes.links(experimental=True)
    return [{"id": str(i.id), "remote": str(i.remote_identity), "network": str(i.network),
             "outbound": bool(i.outbound), "local_endpoint": text(i.local_endpoint),
             "remote_endpoint": text(i.remote_endpoint)} for i in infos]


async def shared(a: dict, b: dict) -> list:
    """The ids of the links a and b both list to each other."""
    ia = {r["id"] for r in await links_of(a) if r["remote"] == b["identity"]}
    ib = {r["id"] for r in await links_of(b) if r["remote"] == a["identity"]}
    return sorted(ia & ib)


async def wait_reflector(r: dict, target: str, deadline: float) -> dict:
    """The reflector's addresses once it holds target, or at deadline (monotonic)."""
    async with await session(r) as c:
        while True:
            addrs = await addrmove.ask(addrmove.local_ips(c), [])
            if target in addrs or time.monotonic() >= deadline:
                return {"addrs": addrs, "moved": target in addrs}
            await asyncio.sleep(1.0)


async def bootstrap(node: dict, words: str | None) -> dict:
    """Claim node with user.bootstrap: generated words, or the given ones."""
    async with await session(node) as c:
        if words is None:
            return await jsonops.value(c, "user.bootstrap?generate=true&out=json")
        return await jsonops.value(c, "user.bootstrap?in=json&out=json",
                                   {"Type": "string16", "Object": words})


async def wait_shared(pairs: list, nodes: dict, clock, deadline: float) -> dict:
    """{"a-b": time} each pair first shared a link, polled every POLL s until all did or deadline."""
    at = {f"{a}-{b}": None for a, b in pairs}
    while True:
        for a, b in pairs:
            key = f"{a}-{b}"
            if at[key] is None and await shared(nodes[a], nodes[b]):
                at[key] = clock()
        if all(v is not None for v in at.values()) or clock() >= deadline:
            return at
        await asyncio.sleep(POLL)


async def introduce(r: dict, n1: dict, user_token: str, clock) -> dict:
    """As the User on the reflector: node1's LAN endpoint, then user.introduce(node1)."""
    out = {"before": sorted({x["remote"] for x in await links_of(r)})}
    async with await session(r, user_token) as u:
        await u.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"], experimental=True)
        sent = clock()
        try:
            docs = await jsonops.call(u, f"user.introduce?identity={n1['identity']}&out=json",
                                      timeout=INTRODUCE_TIMEOUT)
            out["answer"] = [d["Type"] for d in docs]
            out["detail"] = [d.get("Object") for d in docs if d["Type"] == "error_message"]
        except AstralError as e:
            out["answer"], out["detail"] = [], [f"{type(e).__name__}: {e}"]
    out.update(sent_at=sent, acked_at=clock())
    return out


async def rows_of(node: dict, ids: dict) -> dict:
    """{role: endpoints node resolves for that role's identity}."""
    out = {}
    async with await session(node) as c:
        for role, identity in ids.items():
            try:
                eps = await c.nodes.resolve_endpoints(identity, experimental=True)
                out[role] = sorted(text(e.endpoint) for e in eps)
            except AstralError as e:
                out[role] = {"error": f"{type(e).__name__}: {e}"}
    return out


async def status_of(node: dict, ids: dict) -> list:
    """node's user.swarm_status once it lists the three, the other two linked, or at STATUS_WAIT."""
    deadline = time.monotonic() + STATUS_WAIT
    async with await session(node) as c:
        while True:
            rows = [{"identity": str(m.identity), "linked": bool(m.linked)}
                    for m in await c.user.swarm_status() if m.identity is not None]
            others = [m for m in rows if m["identity"] != node["identity"]]
            full = {m["identity"] for m in rows} == set(ids.values())
            if (full and all(m["linked"] for m in others)) or time.monotonic() >= deadline:
                return rows
            await asyncio.sleep(1.0)


async def observe(nodes: dict) -> dict:
    ids = {k: nodes[k]["identity"] for k in ROLES}
    return {"endpoints": {k: await rows_of(nodes[k], ids) for k in ROLES},
            "links": {k: await links_of(nodes[k]) for k in ROLES},
            "swarm": {k: await status_of(nodes[k], ids) for k in ROLES}}


async def form(nodes: dict) -> dict:
    """Steps 2 to 4: the claims, the nearby link, the introduction."""
    n1, n2, r = (nodes[k] for k in ROLES)
    first = await bootstrap(n1, None)
    second = await bootstrap(n2, first["Mnemonic"])
    t0 = time.monotonic()
    clock = lambda: round(time.monotonic() - t0, 2)  # noqa: E731
    before = await shared(n1, n2)
    third = await bootstrap(r, first["Mnemonic"])
    lan = await wait_shared([("node1", "node2")], nodes, clock, LAN_WAIT)
    intro = await introduce(r, n1, third["Token"], clock)
    found = await wait_shared([("reflector", "node1"), ("reflector", "node2")], nodes,
                              clock, intro["acked_at"] + INTRODUCE_WAIT)
    return {"claims": {"node1": first["Identity"], "node2": second["Identity"],
                       "reflector": third["Identity"]},
            "before": before, "lan": lan, "introduce": intro, "found": found}


async def main():
    nodes = load()["nodes"]
    move = addrmove.move_args(Path(__file__).parent)
    target = addrmove.targets(nodes["reflector"], move)[-1]
    moved = await wait_reflector(nodes["reflector"], target,
                                 time.monotonic() + move["in"] + MOVE_MARGIN)
    async with AsyncExitStack() as stack:
        taps = {k: await stack.enter_async_context(LogTap(nodes[k])) for k in ROLES}
        fact = await form(nodes)
        fact.update(await observe(nodes))
        fact["log"] = {k: t.find(*NEEDLES) for k, t in taps.items()}
        fact["taps"] = {k: t.state() for k, t in taps.items()}
    fact.update(target=target, moved=moved)
    write_facts({"forms_and_finds": fact})
    print(f"driver: reflector at {moved}; node1-node2 linked {fact['lan']}; introduce "
          f"{fact['introduce']['answer']} at {fact['introduce']['acked_at']} s; found {fact['found']}")


asyncio.run(main())
