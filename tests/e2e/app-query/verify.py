#!/usr/bin/env python3
"""Oracle: the app's answer was right and its side effect is on node1.

The app is gone by the time this runs, so there is nothing to interrogate —
only what it left. Both checks start from the payload the driver recorded and
recompute the id independently, so neither one asks the app what it did.

The two together are what make this about routing. The answer alone could come
from an app that computed a hash and stored nothing; the object alone could
have been put there by anything. An answer that matches the bytes *and* those
bytes in node1's repository is a query that reached the app, was served under
an identity that could write, and came back.
"""
import asyncio

import astral
from astral.objectid import object_id_of_bytes

from lib.sessionio import as_bytes, load


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    payload = facts["payload"].encode()
    expected = str(object_id_of_bytes(payload))

    assert facts["answered_id"] == expected, (
        f"the app answered {facts['answered_id']} for a payload that hashes to "
        f"{expected} — it did not absorb what node2 sent it")

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as c:
        present = await c.objects.contains("local", expected)
        assert present, (
            f"node1 does not hold {expected} — the app answered correctly and "
            "stored nothing, so nothing proves the query reached it")
        got = as_bytes(await c.objects.load(expected, repo="local"))

    assert got == payload, (
        f"node1 holds {expected} but it reads back as {got!r}, not the "
        f"{len(payload)} B node2 sent")

    print(f"oracle: node2's query reached the app on node1 — it answered "
          f"{expected[:16]}… and node1 holds those exact {len(payload)} B")


asyncio.run(main())
