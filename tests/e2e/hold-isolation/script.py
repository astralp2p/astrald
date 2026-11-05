#!/usr/bin/env python3
"""Driver: three apps hold four objects, then the User purges the repo.

The User stores X, Y, Z and W, registers apps A and B with
`mod.auth.store_objects_action` as a grant and app N with nothing, and then:

- A and B hold X; A unholds X. Only B's hold should remain.
- A holds Y for EXPIRY. The hold should lapse before the purge.
- A holds Z indefinitely, then again for REHOLD. The indefinite hold should
  win the merge (`HoldObject` in `mod/apphost/src/db.go`).
- N tries to hold W and should be refused `not permitted`
  (`op_hold_object.go:36`).
- B unholds Z, which it never held. The node answers `ack` and A's hold on Z
  should stay.

The driver sleeps WAIT, lists each app's holds, and purges `local` as the
User. It records what it saw as facts, and `verify.py` checks the
repository and each app's holds itself.

The payloads are typed `astral.Query` records for the reason hold-purge's
README gives: purge frees only objects the objects module tracks, and an
untyped blob is never tracked. With raw bytes nothing would be purged, and a
broken hold would look the same as a working one.
"""
import asyncio
import datetime

import astral
from astral.errors import AstralError
from astral.object import Query

from lib.sessionio import load, write_facts

STORE = "mod.auth.store_objects_action"

# why: an expiry the purge must outlive, and a re-hold shorter than it. The
# live run used 2 s / 1 s / 3 s. WAIT counts from after Z's re-hold, so Y has
# been held at least WAIT when the purge runs. The 1 s margin is wide on a
# local or Docker node, where an op on the open connections takes
# milliseconds. If a run flakes, widen WAIT rather than drop an assertion.
EXPIRY = datetime.timedelta(seconds=2)
REHOLD = datetime.timedelta(seconds=1)
WAIT = 3


async def attempt(coro) -> str:
    """`ack` when the op answered ack, else the error text the node sent."""
    try:
        await coro
        return "ack"
    except AstralError as e:
        return str(e)


async def held(app) -> list[str]:
    return sorted(str(i) for i in await app.apphost.list_held_objects())


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    endpoint = n1["endpoint"]

    async with await astral.connect(endpoint, token=doc["facts"]["user_token"]) as u:
        ids = [str(i) for i in await u.objects.store(
            *(Query(query_string=f"hold-isolation: object {n}") for n in "XYZW"),
            repo="local")]
        assert len(set(ids)) == 4, f"the four payloads collided: {ids}"
        X, Y, Z, W = ids
        tokens = {
            "A": await u.apphost.register(name="hold-isolation-a", grant_permits=[STORE]),
            "B": await u.apphost.register(name="hold-isolation-b", grant_permits=[STORE]),
            "N": await u.apphost.register(name="hold-isolation-n"),
        }

    apps = {k: {"identity": str(t.identity), "token": t.token} for k, t in tokens.items()}
    lists = {}

    async with await astral.connect(endpoint, token=apps["A"]["token"]) as a, \
            await astral.connect(endpoint, token=apps["B"]["token"]) as b, \
            await astral.connect(endpoint, token=apps["N"]["token"]) as n:
        await a.apphost.hold_object(X)
        await b.apphost.hold_object(X)
        await a.apphost.unhold_object(X)

        await a.apphost.hold_object(Y, duration=EXPIRY)

        await a.apphost.hold_object(Z)
        await a.apphost.hold_object(Z, duration=REHOLD)

        apps["N"]["hold_W"] = await attempt(n.apphost.hold_object(W))
        apps["B"]["unhold_Z"] = await attempt(b.apphost.unhold_object(Z))

        # Taken before any hold can lapse, so Y missing later means it
        # expired, not that it was never held.
        lists["A_before"] = await held(a)

        await asyncio.sleep(WAIT)

        lists["A"] = await held(a)
        lists["B"] = await held(b)
        lists["N"] = await held(n)

    async with await astral.connect(endpoint, token=doc["facts"]["user_token"]) as u:
        purged = [str(i) for i in await u.objects.purge("local")]

    write_facts({
        "ids": dict(zip("XYZW", ids)),
        "apps": apps,
        "lists": lists,
        "purged": purged,
    })
    print(f"driver: N hold W -> {apps['N']['hold_W']!r}; "
          f"B unhold Z -> {apps['B']['unhold_Z']!r}; "
          f"A held {len(lists['A_before'])} then {len(lists['A'])}, "
          f"B {len(lists['B'])}; purge freed {len(purged)} object(s)")


asyncio.run(main())
