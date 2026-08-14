#!/usr/bin/env python3
"""Oracle: the dropped file is an object, under the id its bytes hash to.

Indexing is eventual. The file is in place before the watch, so it is the
initial background scan that must find it — measured at about 23 ms, and the
fsnotify debounce this test never triggers is three seconds — but eventual is
eventual, so this polls to a deadline rather than asserting instantly. What it must never
do is poll for something the driver told it: the id comes from the payload,
computed here, so the question asked of node1 is "do you hold these bytes",
never "do you hold what your indexer decided to call them".

Two claims, and the second is the one that says a *file* was indexed rather
than an object appearing by some other route: the repository serves the bytes,
and `objects.describe` places them at a path on this node.

astral-py binds no `fs` module, so `mod.fs.file_location` is a type it was
never built against and the descriptor would arrive undecodable. The schema is
fetched from the node with `objects.learn` rather than the descriptor being
read as opaque bytes — a substring check against a raw payload would pass on a
descriptor that merely mentioned the path somewhere. A learned type arrives as
a RuntimeRecord whose fields carry their wire names; `.get("Path")` reads them
and answers None for a name the schema does not have, which is what turns a
schema that drifted into a readable assertion rather than an AttributeError.
"""
import asyncio

import astral
from astral.objectid import object_id_of_bytes

from lib.sessionio import as_bytes, load

DEADLINE = 20.0
INTERVAL = 0.5
LOCATION = "mod.fs.file_location"


async def wait_for(c, repo, oid):
    loop = asyncio.get_running_loop()
    end = loop.time() + DEADLINE
    while True:
        if await c.objects.contains(repo, oid):
            return True
        if loop.time() >= end:
            return False
        await asyncio.sleep(INTERVAL)


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    payload = facts["payload"].encode()
    repo = facts["repo"]
    oid = str(object_id_of_bytes(payload))

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        indexed = await wait_for(c, repo, oid)
        assert indexed, (
            f"{repo!r} does not hold {oid[:16]}… after {DEADLINE:.0f}s — the "
            f"file at {facts['file_path']} was never indexed")

        got = as_bytes(await c.objects.load(oid, repo=repo))
        assert got == payload, (
            f"{repo!r} holds {oid[:16]}… but serves {got!r}, not the "
            f"{len(payload)} B the file carries")

        # The node is the authority on its own types; learn it, then read it.
        await c.objects.learn(LOCATION)
        located = [d for d in await c.objects.describe(oid)
                   if getattr(d.data, "ASTRAL_TYPE", "") == LOCATION]

    assert located, (
        f"{oid[:16]}… is in the repository but no {LOCATION} describes it — "
        "nothing says these bytes came from a file")

    paths = [str(d.data.get("Path")) for d in located]
    assert facts["file_path"] in paths, (
        f"{LOCATION} puts {oid[:16]}… at {paths}, not at "
        f"{facts['file_path']}")

    nodes = {str(d.data.get("NodeID")) for d in located}
    assert n1["identity"] in nodes, (
        f"{LOCATION} names {sorted(nodes)} as holding {oid[:16]}…, not node1 "
        f"({n1['identity']})")

    print(f"oracle: the file dropped into the watched directory is "
          f"{oid[:16]}… in {repo!r}, serves its exact {len(payload)} B, and "
          f"{LOCATION} places it at {facts['file_path']} on node1")


asyncio.run(main())
