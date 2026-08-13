#!/usr/bin/env python3
"""Driver: two objects on node1, one of them held, then a purge of the repo.

`objects.purge` frees a repository oldest-read-first and skips anything a
registered holder keeps alive. Both objects are read once before the hold, so
the reads journal has an entry for each and the ordering the purge walks is
defined rather than incidental.

**The payloads carry an astral stamp, and that is load-bearing.** Purge walks
the module's tracking table, and a row is seeded only for a payload the module
recognises as an astral object: `Module.Store` seeds a typed store
(`mod/objects/src/op_store.go:32`, `module.go:125`), and `objects.create`
seeds too when the committed bytes carry a valid stamp (`op_create.go:71` via
`module.go:194-201`). An unstamped blob gets no row either way, and astrald
says so in its own source — "objects without a tracking row are not
purge-eligible. accepted limitation for now" (`module.go:100-101`).

Raw bytes here would make the test green for the wrong reason: nothing would
be purged because nothing *could* be, and the hold would never be consulted at
all. The first draft did exactly that and the purge freed zero objects.

`astral.Query` is the payload type for want of a plainer one: it is a core
registered record with a free-text field, so two of them with different text
are two distinct objects. Nothing about the test depends on what it means.

Everything runs on one connection as the User, the identity the oracle reads
back with. Note that this is hygiene, not a property the test can check: the
hold is *recorded* per caller (`mod/apphost/src/op_hold_object.go:39`) but
*consulted* identity-blind (`db.ObjectHeld` filters on object and expiry
only), so a hold placed under any identity would protect the object here.
"""
import asyncio

import astral
from astral.object import Query

from lib.sessionio import load, write_facts

HELD = "hold-purge: this one is held and must survive the purge"
UNHELD = "hold-purge: this one is unheld and must not survive it"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        held_id, unheld_id = (str(i) for i in await c.objects.store(
            Query(query_string=HELD),
            Query(query_string=UNHELD),
            repo="local"))
        assert held_id != unheld_id, "the two payloads collided into one object"

        # Both objects carry a read before the hold. This is belt and braces,
        # not a precondition: db.Create stamps read_at at store time and
        # (read_at, height) is a total order regardless, so purge-eligibility
        # never depended on a read (mod/objects/src/db.go:47-58).
        await c.objects.load(held_id, repo="local")
        await c.objects.load(unheld_id, repo="local")

        # duration omitted is a permanent hold, not a defaulted one.
        await c.apphost.hold_object(held_id)

        purged = [str(i) for i in await c.objects.purge("local")]

    write_facts({
        "held_id": held_id,
        "unheld_id": unheld_id,
        "purged_ids": purged,
    })
    print(f"driver: held {held_id[:16]}…, left {unheld_id[:16]}… unheld; "
          f"purge freed {len(purged)} object(s)")


asyncio.run(main())
