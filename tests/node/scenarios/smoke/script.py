#!/usr/bin/env python3
"""Driver: touch the node anonymously — the cheapest possible real query."""
import asyncio

import astral

from lib.sessionio import load


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"]) as c:      # anonymous
        specs = await c.shell.spec()
        assert specs, "empty op catalog"
        print(f"driver: node1 exposes {len(specs)} ops")


asyncio.run(main())
