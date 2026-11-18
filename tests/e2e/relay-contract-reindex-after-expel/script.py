#!/usr/bin/env python3
"""Driver: node1 holds an app contract of node2, then the User expels node2.

node2 registers an app, and its relay contract reaches node1, which indexes
and stores it. node1 registers an app of its own. Then the User expels node2.
The runner stops node1, `while_down.py` empties node1's auth index, and node1
starts again and rebuilds the index from its repository.
"""
import asyncio
import time

import astral

from lib import jsonops
from lib.sessionio import load, write_facts

CONVERGE = 30.0


async def hosts(c, app: str) -> list:
    docs = await jsonops.call(c, f"apphost.hosts?app={app}&out=json")
    return [d["Object"] for d in docs if d["Type"] == "identity"]


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        remote_app = str((await c.apphost.register()).identity)
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        local_app = str((await c.apphost.register()).identity)

        # why a wait: apphost.register on node2 pushes its contract to node1 in
        # the background (mod/apphost/src/op_register.go).
        deadline = time.monotonic() + CONVERGE
        known = await hosts(c, remote_app)
        while n2["identity"] not in known and time.monotonic() < deadline:
            await asyncio.sleep(0.5)
            known = await hosts(c, remote_app)
        assert n2["identity"] in known, (
            f"node1 never named node2 for its app {remote_app}: {known}")

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        await c.user.expel(n2["identity"])

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        before_wipe = await hosts(c, remote_app)

    write_facts({"reindex_after_expel": {
        "remote_app": remote_app, "local_app": local_app,
        # note: the index row outlives the expulsion until the index is
        # rebuilt; recorded, not judged.
        "hosts_after_expel_before_wipe": before_wipe}})
    print(f"driver: node2's app {remote_app[:16]}… known on node1, node2 expelled")


asyncio.run(main())
