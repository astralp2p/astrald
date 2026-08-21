#!/usr/bin/env python3
"""Driver: node1, as the User, stores a payload ON the peer, not locally.

Port of the netsim object-store flow with --target node2. The write is routed
to node2, so the object lands in the peer's repository and node1 only learns
its id.
"""
import asyncio
from pathlib import Path

import astral

from lib.sessionio import load, write_facts

PAYLOAD = Path(__file__).resolve().parent / "payload.txt"


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    user_token = doc["facts"]["user_token"]
    payload = PAYLOAD.read_bytes()

    # why: create + write + commit, not objects.store — store canonicalizes a
    # typed object and refuses raw bytes with "empty type".
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        async with c.objects.create(target=n2["identity"]) as w:
            await w.write(payload)
            object_id = str(await w.commit())

    write_facts({"peer_object_id": object_id})
    print(f"driver: stored {len(payload)} B on node2 as {object_id[:16]}…")


asyncio.run(main())
