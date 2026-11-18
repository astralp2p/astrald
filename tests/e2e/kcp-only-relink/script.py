#!/usr/bin/env python3
"""Driver: watch node2 restart while tcp between the siblings is dropped.

block-tcp dropped every tcp packet between node1 and node2 and closed their
tcp links, so kcp over UDP is the only path between them. restart-astrald
restarts node2's astrald --in seconds after it ran. The driver polls node2
every POLL s: whether its apphost answers, and once it is back, which links
it holds to node1. It opens a log tap on node2 as soon as node2 answers again,
and keeps one on node1 throughout.

why a timer and polls: a VM world restarts a node only through an op, which
runs before the driver, and reaches no node file, so the driver has to be
watching when the node goes down and comes back.

The driver acts and judges nothing. It records the `kcp_relink` fact, times
in seconds since the driver began, on the host's clock, and the tapped
link-task lines on each node's clock.
"""
import asyncio
import shlex
import time
import tomllib
from pathlib import Path

import astral

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

POLL = 0.2
CONNECT = 2.0
# why: past the restart, a restart that takes long, and a relink that fails
MARGIN = 120.0
LINKED_FOR = 5.0
NEEDLES = ("starting to maintain link", "established", "restored after",
           "trying to reconnect", "is broken")


def restart_in() -> int:
    """--in of this test's restart-astrald op."""
    ops = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["ops"]
    words = next(shlex.split(o) for o in ops if o.startswith("restart-astrald"))
    return int(words[words.index("--in") + 1])


async def answers(node: dict) -> bool:
    try:
        async def ask():
            async with await astral.connect(node["endpoint"], token=node["token"]) as c:
                await c.call("apphost.whoami", timeout=CONNECT)
        await asyncio.wait_for(ask(), CONNECT)
        return True
    except Exception:  # noqa: BLE001 — a node that does not answer is the observation
        return False


async def links_to(node: dict, peer: str) -> list:
    async def ask():
        async with await astral.connect(node["endpoint"], token=node["token"]) as c:
            return [{"network": str(l.network), "outbound": bool(l.outbound), "id": str(l.id)}
                    for l in await c.nodes.links(experimental=True)
                    if str(l.remote_identity) == peer]
    try:
        return await asyncio.wait_for(ask(), CONNECT)
    except Exception:  # noqa: BLE001
        return []


async def watch(n2: dict, id1: str, t0: float, deadline: float) -> dict:
    """down, up and the first kcp link on node2, in s since t0, and node2's tap."""
    seen = {"down": None, "up": None, "kcp": None, "links": [], "log2": [], "tap2": None}
    clock = lambda: round(time.monotonic() - t0, 2)  # noqa: E731
    while seen["down"] is None and clock() < deadline:
        if not await answers(n2):
            seen["down"] = clock()
        await asyncio.sleep(POLL)
    while seen["down"] is not None and seen["up"] is None and clock() < deadline:
        if await answers(n2):
            seen["up"] = clock()
        else:
            await asyncio.sleep(POLL)
    if seen["up"] is None:
        return seen
    async with LogTap(n2) as t2:
        while clock() < deadline:
            seen["links"] = await links_to(n2, id1)
            if seen["kcp"] is None and any(l["network"] == "kcp" for l in seen["links"]):
                seen["kcp"] = clock()
            if seen["kcp"] is not None and clock() >= seen["kcp"] + LINKED_FOR:
                break
            await asyncio.sleep(POLL)
        seen["log2"] = [e for e in t2.find(*NEEDLES) if id1 in e["text"]]
        seen["tap2"] = t2.state()
    return seen


async def main():
    nodes = load()["nodes"]
    n1, n2 = nodes["node1"], nodes["node2"]
    t0 = time.monotonic()
    span = restart_in()
    async with LogTap(n1) as t1:
        before = await links_to(n2, n1["identity"])
        seen = await watch(n2, n1["identity"], t0, span + MARGIN)
        log1 = [e for e in t1.find(*NEEDLES) if n2["identity"] in e["text"]]
        tap1 = t1.state()
    write_facts({"kcp_relink": {"restart_in": span, "before": before, **seen,
                                "log1": log1, "tap1": tap1,
                                "watched": round(time.monotonic() - t0, 2)}})
    print(f"driver: node2 down at {seen['down']} s, up at {seen['up']} s, "
          f"kcp link to node1 at {seen['kcp']} s; links {seen['links']}")


asyncio.run(main())
