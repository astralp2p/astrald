#!/usr/bin/env python3
"""Oracle: node1 hears node2's answers exactly as the zone and sources allow.

The oracle repeats every call the driver made and checks the source of each
answer against the table in README.md, then checks the driver reported the same
counts. Each call is timed: one well inside the one-minute op timeout shows no
peer asked back.
"""
import asyncio
import time

import astral

from lib.sessionio import load

LOCATION = "mod.fs.file_location"
WANT = {  # label: (describes from node2, searches from node2)
    "dvn": (1, 1),
    "sources": (2, 1),
    "sources_only": (1, 1),
    "dv": (0, 0),
}


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    facts = doc["facts"]
    name, oid = facts["watched_name"], facts["watched_object_id"]
    src = n2["identity"]

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        await c.objects.learn(LOCATION)
        for label, extra in [("dvn", "zone=dvn"),
                             ("sources", f"zone=dvn&sources={src}"),
                             ("sources_only", f"zone=dvn&sources={src}&sources_only=true"),
                             ("dv", "zone=dv")]:
            start = time.monotonic()
            described = await c.call(f"objects.describe?id={oid}&{extra}")
            found = await c.call(f"objects.search?q={name}&{extra}")
            took = time.monotonic() - start

            from_n2 = (sum(str(d.source_id) == src for d in described),
                       sum(str(r.source_id) == src and str(r.object_id) == oid
                           for r in found))
            assert from_n2 == WANT[label], (
                f"{label}: node2 answered (describe, search) = {from_n2}; "
                f"want {WANT[label]}")
            assert all(str(d.source_id) == src for d in described), (
                f"{label}: a descriptor came from a source other than node2")
            assert took < 30, f"{label}: the calls took {took:.1f}s"

            reported = facts["answers"][label]
            assert (reported["describe"].count(src),
                    reported["search"].count(src)) == WANT[label], (
                f"{label}: the driver reported {reported}")

    print(f"oracle: node2's answers reached node1 as {WANT} "
          f"(describe, search) for {oid[:16]}…")


asyncio.run(main())
