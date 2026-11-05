#!/usr/bin/env python3
"""Oracle: each app's holds stayed its own, expired on time, and the purge obeyed them.

The repository decides, not the driver. The oracle asks node1 which of X, Y,
Z and W are still in `local`, loads each survivor and checks that its
re-encoding hashes to the id it was stored under. Then it connects as A, B
and N, each with its own token, and lists each app's holds itself.
`list_held_objects` is scoped to the caller, so each list is that app's view
and nobody else's.

What each result rules out:

- X survives only through B's hold. If A's unhold of X also deleted B's hold,
  X is purged.
- Y is gone and A no longer lists it, though A listed it right after the
  hold. If the hold never expired, Y survives the purge.
- Z survives and A still lists it. If the 1 s re-hold overwrote the indefinite
  hold, it lapsed before the purge and Z is gone. If B's unhold of Z deleted
  A's hold, Z is gone too.
- W is gone and N lists nothing, so N's refused hold placed no hold.

The survivors (X, Z) and the purged objects (Y, W) are both required. A purge
that frees nothing would leave X and Z in place without any hold being
consulted.

The purge's own list of freed ids is reported, not trusted.
"""
import asyncio

import astral
from astral.errors import AstralError
from astral.objectid import object_id_of

from lib.sessionio import load


def short(i: str) -> str:
    return f"{i[:16]}…"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    facts = doc["facts"]
    ids, apps, lists = facts["ids"], facts["apps"], facts["lists"]
    X, Y, Z, W = (ids[k] for k in "XYZW")
    assert len({X, Y, Z, W}) == 4, f"the driver recorded an object twice: {ids}"
    name = {v: k for k, v in ids.items()}

    def names(objs) -> list[str]:
        return sorted(name.get(o, short(o)) for o in objs)

    faults = []

    async with await astral.connect(n1["endpoint"],
                                    token=facts["user_token"]) as u:
        present = {k: await u.objects.contains("local", v) for k, v in ids.items()}
        for k in "XZ":
            if not present[k]:
                continue
            try:
                got = await u.objects.load(ids[k], repo="local")
            except AstralError as e:
                faults.append(f"{k} survived the purge but no longer loads: "
                              f"{type(e).__name__}: {e}")
                continue
            if str(object_id_of(got)) != ids[k]:
                faults.append(f"{k} survived but reads back as {object_id_of(got)}, "
                              f"not {ids[k]}")

    held = {}
    for k in "ABN":
        async with await astral.connect(n1["endpoint"], token=apps[k]["token"]) as app:
            held[k] = names(str(i) for i in await app.apphost.list_held_objects())

    if not present["X"]:
        faults.append("X was purged: A's unhold of X also deleted B's hold")
    if present["Y"]:
        faults.append("Y survived the purge: A's 2 s hold on it never expired")
    if not present["Z"]:
        faults.append("Z was purged: A's indefinite hold did not survive its own "
                      "1 s re-hold, or B's unhold of Z deleted A's hold")
    if present["W"]:
        faults.append("W survived the purge: N's refused hold was placed anyway")

    if held["A"] != ["Z"]:
        faults.append(f"A holds {held['A']}, not exactly [Z]")
    if held["B"] != ["X"]:
        faults.append(f"B holds {held['B']}, not exactly [X]")
    if held["N"] != []:
        faults.append(f"N, with no store permit, holds {held['N']}")

    # The driver's records of what the node answered. Each is corroborated
    # above: N's refusal by W purged and N holding nothing, B's no-op unhold by
    # Z surviving and A still holding it, the expiry by Y listed before it.
    if "not permitted" not in apps["N"]["hold_W"]:
        faults.append(f"N's hold on W was answered {apps['N']['hold_W']!r}, "
                      "not `not permitted`")
    if apps["B"]["unhold_Z"] != "ack":
        faults.append(f"B's unhold of Z, a hold it never had, was answered "
                      f"{apps['B']['unhold_Z']!r}, not `ack`")
    if names(lists["A_before"]) != ["Y", "Z"]:
        faults.append(f"right after its holds A listed {names(lists['A_before'])}, "
                      "not [Y, Z]: Y gone later proves nothing about expiry")
    for k in "ABN":
        if names(lists[k]) != held[k]:
            faults.append(f"after the wait the driver saw {k} hold "
                          f"{names(lists[k])}; it holds {held[k]} now")

    assert not faults, "; ".join(faults)

    print(f"oracle: X and Z survived intact, Y and W are gone; A holds [Z], "
          f"B holds [X], N holds nothing and was refused; the purge reported "
          f"{names(facts['purged'])} freed")


asyncio.run(main())
