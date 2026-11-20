#!/usr/bin/env python3
"""Driver: claim node1, find the unowned node2 on tcp 1800 in node1's nearby list, adopt it by its nearby alias.

The driver acts and judges nothing. node2 listens on tcp 1800 and kcp 1810
(test.toml), so no transport of it listens on a default port. The driver
claims node1 for a User with generated words, asks node1 to scan, and waits
for node1's nearby.list to carry a status of node2 that arrived after the
scan. It reads node2's alias from that status's public profile, and on node1
resolves `.<alias>` and node2's endpoints and lists its links to node2. As
the User it then adopts `.<alias>`, and lists node1's links to node2 again.

why no endpoint and no alias: what is under test is that nearby alone carries
the adoption to a node off the default ports: the name from node2's public
profile, and node2's address from nearby's endpoint resolver, the source IP
with the ports node2's Visible status carries in its mod.nearby.listen_ports
(mod/nearby/src/endpoint_resolver.go). Nothing here, and nothing in the lab,
gives node1 an endpoint or an alias for node2.

why the links right after the adoption: node2 is unowned and node1 is
Stealth, so node2 cannot identify node1 and has no endpoint to dial it at.
Every link between them right after user.adopt is one node1 dialed, at an
endpoint nearby resolved.

why the scan: as nearby-adopt's, node2 answers it within the 2 s reply window
(mod/nearby/src/scan_reply.go), where its periodic status could take 95 s.
"""
import asyncio
import time
import tomllib
from pathlib import Path

import astral
from astral.errors import AstralError

from lib import jsonops, linkview, nearby
from lib.sessionio import load, write_facts

# why: node2 answers a scan at once, and sends its status every 85-95 s
# anyway; past both, node1 does not hear it
LISTED = 120.0
POLL = 1.0
CONFIG = tomllib.loads((Path(__file__).parent / "test.toml").read_text())["config"]


def failure(e: Exception) -> dict:
    return {"error": f"{type(e).__name__}: {e}"}


def endpoint(e) -> str | None:
    return None if e is None else f"{e.network()}:{e.address()}"


async def links_to(c, peer: str) -> list:
    """node's links to peer as linkview rows, with both endpoints."""
    return [{**linkview.row(i), "local_endpoint": endpoint(i.local_endpoint),
             "remote_endpoint": endpoint(i.remote_endpoint)}
            for i in await c.nodes.links(experimental=True)
            if str(i.remote_identity) == peer]


async def listed_after_scan(c, node: str) -> tuple:
    """(scan answer, node's entry before the scan, its first newer entry, seconds waited)."""
    before = nearby.of(await nearby.statuses(c), node)
    scan = await nearby.scan(c)
    t0 = time.monotonic()
    deadline = t0 + LISTED
    while True:
        entry = nearby.of(await nearby.statuses(c), node)
        if entry and (before is None or entry["nonce"] != before["nonce"]):
            return scan, before, entry, time.monotonic() - t0
        if time.monotonic() >= deadline:
            return scan, before, None, time.monotonic() - t0
        await asyncio.sleep(POLL)


async def find(c, n2: dict) -> dict:
    """What node1 learns of node2 from nearby before the adoption."""
    facts = {}
    scan, before, listed, waited = await listed_after_scan(c, n2["identity"])
    facts.update(scan=scan, before=before, listed=listed, waited=round(waited, 1))
    profile = listed["profile"] if listed else None
    facts["alias"] = alias = profile["alias"] if profile else ""
    if alias:
        try:
            facts["resolved"] = {"identity": str(await c.dir.resolve("." + alias))}
        except AstralError as e:
            facts["resolved"] = failure(e)
    try:
        eps = await c.nodes.resolve_endpoints(n2["identity"], experimental=True)
        facts["endpoints"] = sorted(f"{e.endpoint.network()}:{e.endpoint.address()}"
                                    for e in eps)
    except AstralError as e:
        facts["endpoints"] = failure(e)
    facts["links_before"] = await links_to(c, n2["identity"])
    return facts


async def main():
    n = load()["nodes"]
    n1, n2 = n["node1"], n["node2"]

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        claim = await jsonops.value(c, "user.bootstrap?generate=true&out=json")
    user = {"identity": claim["Identity"], "token": claim["Token"]}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        facts = await find(c, n2)
    alias = facts["alias"]

    if alias:
        async with await astral.connect(n1["endpoint"], token=user["token"]) as c:
            try:
                signed = await c.user.adopt("." + alias)
                facts["adopted"] = {"issuer": str(signed.contract.issuer),
                                    "subject": str(signed.contract.subject)}
            except AstralError as e:
                facts["adopted"] = failure(e)
        async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
            facts["links_after"] = await links_to(c, n2["identity"])

    write_facts({"nearby_ports_adopt": {
        "user": user, "ports": CONFIG["node2"], **facts}})
    print(f"driver: node1 claimed for {user['identity'][:16]}…; node2 "
          f"{'listed as .' + alias if alias else 'never listed with an alias'} "
          f"after {facts['waited']:.0f}s at {facts['endpoints']}; adopt answered "
          f"{facts.get('adopted')}")


asyncio.run(main())
