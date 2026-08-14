#!/usr/bin/env python3
"""Oracle: the held object survived the purge and the unheld one did not.

The verdict comes from the repository, never from the driver's account of what
the purge said it freed. A purge that returns a convincing list and deletes the
wrong thing — or deletes nothing — is exactly the failure a test that trusted
the returned list would miss, and the returned ids are carried as facts for the
report rather than as evidence.

Both halves are required. "The held object is still there" alone passes on a
node whose purge does nothing at all, which is the same silent failure as a
purge that ignores holds — one destroys data, the other never reclaims any.

The surviving bytes are checked against the id that asked for them, not against
a payload this file knows. Two drivers write this flow — the script stores what
it likes, and an operator told to "store two different short texts" stores
something else — so an oracle holding a copy of the payload would be judging
the script and merely tolerating the agent. The id is a content hash, which
makes the object prove itself under either.
"""
import asyncio

import astral
from astral.errors import AstralError
from astral.objectid import object_id_of

from lib.sessionio import load


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    held_id = facts["held_id"]
    unheld_id = facts["unheld_id"]
    assert held_id != unheld_id, "the driver recorded one object twice"

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        held_present = await c.objects.contains("local", held_id)
        unheld_present = await c.objects.contains("local", unheld_id)

        got, unreadable = None, None
        if held_present:
            try:
                got = await c.objects.load(held_id, repo="local")
            except AstralError as e:
                unreadable = f"{type(e).__name__}: {e}"

    faults = []
    if not held_present:
        faults.append(
            f"the purge freed the held object {held_id[:16]}… — a hold that is "
            "recorded and not consulted destroys an app's data")
    elif unreadable:
        faults.append(
            f"the held object survived the purge but no longer reads: "
            f"{unreadable}")
    elif str(object_id_of(got)) != held_id:
        faults.append(
            f"the held object survived but reads back as something else: "
            f"{object_id_of(got)}, not {held_id}")
    if unheld_present:
        faults.append(
            f"the purge left the unheld object {unheld_id[:16]}… in place — it "
            "reclaimed nothing, so surviving proves nothing about the hold")

    assert not faults, "; ".join(faults)

    print(f"oracle: {held_id[:16]}… survived the purge intact "
          f"(re-encodes to its own id) and {unheld_id[:16]}… is gone; "
          f"the purge reported {len(facts['purged_ids'])} freed")


asyncio.run(main())
