#!/usr/bin/env python3
"""Driver: claim node1, find the unowned node2 in node1's nearby list, adopt it by its nearby alias.

The driver acts and judges nothing. It claims node1 for a User with generated
words, asks node1 to scan, and waits for node1's nearby.list to carry a status
of node2 that arrived after the scan. It reads node2's alias from that
status's public profile, and on node1 resolves `.<alias>` and node2's
endpoints. As the User it then adopts `.<alias>`.

why no endpoint and no alias: what is under test is that nearby alone carries
the adoption: the name from node2's public profile, resolved by nearby's
`.alias` resolver (mod/nearby/src/identity_resolver.go), and node2's address
from nearby's endpoint resolver, tcp <source IP>:1791 and kcp <source
IP>:1792 (mod/nearby/src/endpoint_resolver.go). Nothing here, and nothing in
the lab, gives node1 an endpoint or an alias for node2.

why the scan: an app showing nearby nodes asks for a fresh view, and the
scan is that request. node2 answers it within the 2 s reply window
(mod/nearby/src/scan_reply.go), where its periodic status could take 95 s.

why the words are a fact: nearby-stealth-meet claims the reflector with them,
so that it is the User's node without holding any contract node1 issued.
"""
import asyncio
import time

import astral
from astral.errors import AstralError

from lib import jsonops, nearby
from lib.sessionio import load, write_facts

# why: node2 answers a scan at once, and sends its status every 85-95 s
# anyway; past both, node1 does not hear it
LISTED = 120.0
POLL = 1.0


def failure(e: Exception) -> dict:
    return {"error": f"{type(e).__name__}: {e}"}


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


async def main():
    n = load()["nodes"]
    n1, n2 = n["node1"], n["node2"]

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        claim = await jsonops.value(c, "user.bootstrap?generate=true&out=json")
    user = {"identity": claim["Identity"], "token": claim["Token"],
            "words": claim["Mnemonic"]}

    facts = {}
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        scan, before, listed, waited = await listed_after_scan(c, n2["identity"])
        facts.update(scan=scan, before=before, listed=listed, waited=round(waited, 1))
        profile = listed["profile"] if listed else None
        alias = profile["alias"] if profile else ""
        facts["alias"] = alias
        if alias:
            try:
                facts["resolved"] = {"identity": str(await c.dir.resolve("." + alias))}
            except AstralError as e:
                facts["resolved"] = failure(e)
        # why before the adoption: afterwards the link and the contract sync
        # add endpoints of their own, and the question is where node1 could
        # find node2 when it had only nearby to ask
        try:
            eps = await c.nodes.resolve_endpoints(n2["identity"], experimental=True)
            facts["endpoints"] = sorted(f"{e.endpoint.network()}:{e.endpoint.address()}"
                                        for e in eps)
        except AstralError as e:
            facts["endpoints"] = failure(e)

    if alias:
        async with await astral.connect(n1["endpoint"], token=user["token"]) as c:
            try:
                signed = await c.user.adopt("." + alias)
                facts["adopted"] = {"issuer": str(signed.contract.issuer),
                                    "subject": str(signed.contract.subject)}
            except AstralError as e:
                facts["adopted"] = failure(e)

    write_facts({"nearby_user": user, "nearby_adopt": facts})
    print(f"driver: node1 claimed for {user['identity'][:16]}…; node2 "
          f"{'listed as .' + alias if alias else 'never listed with an alias'} "
          f"after {waited:.0f}s; adopt answered {facts.get('adopted')}")


asyncio.run(main())
