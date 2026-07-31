#!/usr/bin/env python3
"""Oracle: the node answers, authenticates, and exposes the core ops."""
import asyncio

import astral

from lib.sessionio import load


async def main():
    n1 = load()["nodes"]["node1"]

    async with await astral.connect(n1["endpoint"]) as c:      # anonymous
        names = {s.name for s in await c.shell.spec()}
    for op in ("user.info", "dir.set_alias", "nodes.add_endpoint"):
        assert op in names, f"missing op {op}"

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        assert c.authenticated, "node token not accepted"
        who = await c.apphost.whoami()
        assert str(who) == n1["identity"], (
            f"whoami {who} != node identity {n1['identity']}")
    print("oracle: spec + auth + whoami ok")


asyncio.run(main())
