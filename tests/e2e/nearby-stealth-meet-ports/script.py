#!/usr/bin/env python3
"""Driver: claim node1 and node2 for one User with user.bootstrap, then wait for them to link through nearby.

The driver acts and judges nothing. node1 listens on tcp 1800 and kcp 1810,
node2 on tcp 1801 and kcp 1811 (test.toml), so no transport of either
listens on a default port. The driver claims node1 with generated words and
node2 with the same words, each with user.bootstrap on a fresh node, so both
broadcast Stealth and neither holds an endpoint or a contract of the other.
While both nodes' logs are recorded, it polls both nodes' links until each
lists one link to the other under the same id, for up to WAIT seconds from
node2's claim. It then records the nearby entries, the stored modes, the
endpoints each node resolves for the other, and the links.

why no scan, no endpoint, no alias and no introduction: what is under test is
that the periodic stealth status alone carries the ports, sealed under the
node identity, to a sibling that recognises the node
(mod/nearby/src/verify.go), and that its user module dials them
(mod/user/src/peer_seen.go).

why node2's claim starts the clock: from then on both nodes are the User's.
node1 broadcasts within one status interval (85-95 s,
mod/nearby/src/send.go), and node2 recognises it; node2's claim changes its
user, and node1 recognises its next status. Either contact can link.
"""
import asyncio
import time
import tomllib
from pathlib import Path

import astral
from astral.errors import AstralError

from lib import jsonops, linkview, nearby
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

# why: two status intervals of at most 95 s each, and margin to see a link
# that forms late; the oracle holds the link to its own bound
WAIT = 240.0
POLL = 2.0
CONFIG = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["config"]
# the user module's nearby contact lines (mod/user/src/peer_seen.go)
NEEDLES = ("nearby peer", "is nearby at")


def endpoint(e) -> str | None:
    return None if e is None else f"{e.network()}:{e.address()}"


async def links_to(node: dict, peer: str) -> list:
    """node's links to peer as linkview rows, with both endpoints."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        infos = await c.nodes.links(experimental=True)
    return [{**linkview.row(i), "local_endpoint": endpoint(i.local_endpoint),
             "remote_endpoint": endpoint(i.remote_endpoint)}
            for i in infos if str(i.remote_identity) == peer]


def shared(a: list, b: list) -> bool:
    """Whether two nodes' rows to each other hold one link under one id."""
    return bool({r["id"] for r in a} & {r["id"] for r in b})


async def bootstrap(node: dict, words: str | None) -> dict:
    """Claim node with user.bootstrap: generated words, or the given ones."""
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        if words is None:
            return await jsonops.value(c, "user.bootstrap?generate=true&out=json")
        return await jsonops.value(c, "user.bootstrap?in=json&out=json",
                                   {"Type": "string16", "Object": words})


async def resolve(node: dict, peer: str):
    async with await astral.connect(node["endpoint"], token=node["token"]) as c:
        try:
            eps = await c.nodes.resolve_endpoints(peer, experimental=True)
        except AstralError as e:
            return {"error": f"{type(e).__name__}: {e}"}
    return sorted(f"{e.endpoint.network()}:{e.endpoint.address()}" for e in eps)


async def observe(nodes: dict, ids: dict) -> dict:
    """What each node holds of the other once the wait is over."""
    out = {"entries": {}, "modes": {}, "endpoints": {}}
    for k, other in (("node1", "node2"), ("node2", "node1")):
        async with await astral.connect(nodes[k]["endpoint"], token=nodes[k]["token"]) as c:
            out["entries"][k] = nearby.of(await nearby.statuses(c), ids[other])
            out["modes"][k] = await nearby.stored_mode(c)
        out["endpoints"][k] = await resolve(nodes[k], ids[other])
    return out


async def wait_link(n1: dict, n2: dict, ids: dict, t0: float) -> tuple:
    """(node1's rows, node2's rows, seconds since t0) once they share a link,
    or at WAIT."""
    while True:
        a = await links_to(n1, ids["node2"])
        b = await links_to(n2, ids["node1"])
        waited = time.monotonic() - t0
        if shared(a, b) or waited >= WAIT:
            return a, b, round(waited, 1)
        await asyncio.sleep(POLL)


async def main():
    nodes = load()["nodes"]
    n1, n2 = nodes["node1"], nodes["node2"]
    ids = {k: nodes[k]["identity"] for k in ("node1", "node2")}

    first = await bootstrap(n1, None)
    async with LogTap(n1) as t1, LogTap(n2) as t2:
        second = await bootstrap(n2, first["Mnemonic"])
        t0 = time.monotonic()
        before = {"node1": await links_to(n1, ids["node2"]),
                  "node2": await links_to(n2, ids["node1"])}
        a, b, waited = await wait_link(n1, n2, ids, t0)
        seen = await observe(nodes, ids)
        taps = {"node1": t1, "node2": t2}

    write_facts({"nearby_ports_meet": {
        "claims": {"node1": first["Identity"], "node2": second["Identity"]},
        "ports": CONFIG, "before": before, "links": {"node1": a, "node2": b},
        "linked": shared(a, b), "waited": waited, **seen,
        "log": {k: t.find(*NEEDLES) for k, t in taps.items()},
        "taps": {k: t.state() for k, t in taps.items()},
    }})
    print(f"driver: node1 and node2 claimed for {first['Identity'][:16]}…; "
          f"linked={shared(a, b)} after {waited:.0f}s")


asyncio.run(main())
