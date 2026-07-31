#!/usr/bin/env python3
"""Driver: node1, as the User, stores a fixed payload in its own repo.

Deterministic port of the netsim object-store flow (--target localnode), where
an AI operator performed the same store. payload.txt is the ground truth the
oracle reads back; the driver never gets to say what it stored.
"""
import asyncio
from pathlib import Path

import astral

from lib.sessionio import load, write_facts

PAYLOAD = Path(__file__).resolve().parent / "payload.txt"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    user_token = doc["facts"]["user_token"]
    payload = PAYLOAD.read_bytes()

    # why: objects.store refuses an untyped blob ("empty type") — raw bytes go
    # through create + write + commit, which is also what the operator's
    # astral-query does behind the prompt.
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        async with c.objects.create() as w:
            await w.write(payload)
            object_id = str(await w.commit())
    write_facts({"object_id": object_id})
    print(f"driver: stored {len(payload)} B on node1 as {object_id[:16]}…")


asyncio.run(main())
