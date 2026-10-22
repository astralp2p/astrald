#!/usr/bin/env python3
"""Driver: touch the node anonymously — the cheapest possible real query.

A token-less session reaches the claim-state probe user.info and nothing off
the anonymous allowlist. The driver records how both answered.
"""
import asyncio

import astral
from astral.errors import AstralError

from lib.sessionio import load, write_facts


async def attempt(coro) -> dict:
    """Run one probe; report whether it was refused, and how it read."""
    try:
        await coro
    except AstralError as e:
        return {"refused": True, "detail": f"{type(e).__name__}: {e}"}
    return {"refused": False, "detail": "answered"}


async def main():
    n1 = load()["nodes"]["node1"]
    async with await astral.connect(n1["endpoint"]) as c:      # anonymous
        info = await attempt(c.user.info())
        spec = await attempt(c.shell.spec())
    write_facts({"smoke": {"user_info": info, "shell_spec": spec}})
    print(f"driver: anonymous user.info {info['detail']}; "
          f"anonymous shell.spec {spec['detail']}")


asyncio.run(main())
