#!/usr/bin/env python3
"""Driver: describe and search on node1 an object only node2 knows about.

node2 gets a run-unique file in a watched directory, so its fs describer and fs
searcher have an answer. node1 then asks through the swarm, through explicit
sources, through sources alone, and locally.
"""
import asyncio
import os
from pathlib import Path

import astral
from astral.objectid import object_id_of_bytes

from lib.sessionio import load, write_facts

REPO = "discover-swarm"
LOCATION = "mod.fs.file_location"
DEADLINE = 20.0


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    session_dir = Path(os.environ["ASTRAL_TESTS_SESSION"]).resolve().parent
    name = f"discover-swarm-{session_dir.parent.name}.txt"
    watch_dir = session_dir / "discover-swarm"
    watch_dir.mkdir(parents=True, exist_ok=True)
    payload = f"discover-swarm probe {name}\n".encode()
    (watch_dir / name).write_bytes(payload)
    oid = str(object_id_of_bytes(payload))

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        await c.call_one(f"fs.new_watch?path={watch_dir}&name={REPO}")
        loop = asyncio.get_running_loop()
        end = loop.time() + DEADLINE
        while not await c.objects.search(name, zone="dv"):
            assert loop.time() < end, f"node2 never indexed {name}"
            await asyncio.sleep(0.5)

    src = n2["identity"]
    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        await c.objects.learn(LOCATION)
        answers = {}
        for label, extra in [("dvn", "zone=dvn"),
                             ("sources", f"zone=dvn&sources={src}"),
                             ("sources_only", f"zone=dvn&sources={src}&sources_only=true"),
                             ("dv", "zone=dv")]:
            described = await c.call(f"objects.describe?id={oid}&{extra}")
            found = await c.call(f"objects.search?q={name}&{extra}")
            answers[label] = {
                "describe": [str(d.source_id) for d in described],
                "search": [str(r.source_id) for r in found
                           if str(r.object_id) == oid],
            }

    write_facts({"watched_name": name, "watched_object_id": oid,
                 "answers": answers})
    print(f"driver: {name} on node2; answers by source: {answers}")


asyncio.run(main())
