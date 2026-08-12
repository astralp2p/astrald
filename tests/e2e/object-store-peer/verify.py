#!/usr/bin/env python3
"""Oracle: the peer's own repository holds the object, byte for byte.

The decisive check runs against node2 directly and pins the repository, so a
copy that stayed on node1 cannot satisfy it.
"""
import asyncio
from pathlib import Path

import astral

from lib.sessionio import as_bytes, load

PAYLOAD = Path(__file__).resolve().parent / "payload.txt"


async def main():
    doc = load()
    n2 = doc["nodes"]["node2"]
    object_id = doc["facts"]["peer_object_id"]
    payload = PAYLOAD.read_bytes()
    assert object_id, "driver recorded no peer_object_id"

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        held = await c.objects.contains("local", object_id)
        got = await c.objects.load(object_id, repo="local")

    assert held, f"node2's local repo does not contain {object_id}"
    assert as_bytes(got) == payload, (
        f"node2's local repo returned {as_bytes(got)!r} != "
        f"stored {payload!r}")
    print(f"oracle: node2's local repo holds {object_id[:16]}… "
          f"with the exact {len(payload)} B")


asyncio.run(main())
