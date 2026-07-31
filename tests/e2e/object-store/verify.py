#!/usr/bin/env python3
"""Oracle: node1's local repo holds the object, byte for byte.

Ported from the netsim object-store verify.py: the decisive check is a
repo-pinned load on the holder matched against payload.txt — the bytes the
driver was given, not the bytes it claims to have written.
"""
import asyncio
from pathlib import Path

import astral

from lib.sessionio import load

PAYLOAD = Path(__file__).resolve().parent / "payload.txt"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    object_id = doc["facts"]["object_id"]
    payload = PAYLOAD.read_bytes()
    assert object_id, "driver recorded no object_id"

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        got = await c.objects.load(object_id, repo="local")

    assert bytes(got) == payload, (
        f"node1's local repo returned {bytes(got)!r} != stored {payload!r}")
    print(f"oracle: node1's local repo holds {object_id[:16]}… "
          f"with the exact {len(payload)} B")


asyncio.run(main())
