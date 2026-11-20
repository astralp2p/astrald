#!/usr/bin/env python3
"""Oracle: an expelled management node is refused every swarm call on node1,
and node1's roster and bans stay as the expulsion left them.

Judged from the driver's record and the oracle's own reads:

- the baseline, read before any call: emn1's roster is emn1, emn2 and emn3,
  and it holds no ban;
- the controls, made with a token for emn2's identity while emn2 was a member:
  `user.list_expelled` and `user.list_siblings` answered, and `user.expel`
  naming the User read "cannot expel the swarm user", the refusal `Expel`
  makes after the call was authorized. They show the token acts as emn2 and
  that emn2's contract carries the permits, so the refusals below are the
  ban's doing;
- after the User expelled emn2: emn1's roster is emn1 and emn3, and its one
  ban names emn2;
- the four calls made then, with the same token, are each refused "not
  permitted", and emn1's roster and bans read after each equal that state:
  the expelled node expelled no one;
- the oracle repeats the three reads and the expel itself with a token of its
  own, so a driver that never called does not pass, and checks that emn3 was
  not banned.
"""
import asyncio

import astral

from lib.sessionio import load

CALLS = ("list_expelled", "list_siblings", "expel_user")
DENIED = "not permitted"
USER_REFUSAL = "cannot expel the swarm user"


async def refusal(coro) -> str:
    try:
        v = await asyncio.wait_for(coro, 20)
    except Exception as e:  # noqa: BLE001 — the refusal text is what is judged
        return str(e)
    raise AssertionError(f"answered, not refused: {v!r}")


async def main():
    doc = load()
    n1 = doc["nodes"]["emn1"]
    f = doc["facts"]
    id1, id2, id3 = (doc["nodes"][n]["identity"] for n in ("emn1", "emn2", "emn3"))
    user_id = f["user_id"]
    assert (f["banned_id"], f["member_id"]) == (id2, id3)

    # --- the record: baseline, controls, expulsion, refusals -------------
    base = f["baseline"]
    assert set(base["roster"]) == {id1, id2, id3} and base["bans"] == [], (
        f"baseline: roster {base['roster']}, bans {base['bans']}; wanted emn1, "
        "emn2 and emn3 with no ban")

    ctl = f["controls"]
    assert set(ctl) == set(CALLS), f"controls recorded: {sorted(ctl)}"
    for name in ("list_expelled", "list_siblings"):
        assert ctl[name]["outcome"] == "answered", (
            f"control {name}: {ctl[name]['outcome']}: {ctl[name]['detail']}; emn2 "
            "was a member and should have been answered")
    assert USER_REFUSAL in ctl["expel_user"]["detail"], (
        f"control expel_user: {ctl['expel_user']['detail']!r}; wanted the refusal "
        f"{USER_REFUSAL!r}, which an authorized caller meets")

    settled = f["expelled"]
    assert set(settled["roster"]) == {id1, id3} and settled["bans"] == [id2], (
        f"after the expulsion: roster {settled['roster']}, bans {settled['bans']}; "
        "wanted emn1 and emn3, and a ban on emn2")

    refusals = f["refusals"]
    assert set(refusals) == set(CALLS) | {"expel_member"}, (
        f"refusals recorded: {sorted(refusals)}")
    for name, r in refusals.items():
        assert r["outcome"] == "refused", (
            f"{name}: {r['outcome']}: {r['detail']}; the expelled node was answered")
        assert DENIED in r["detail"], (
            f"{name}: refused with {r['detail']!r}, not {DENIED!r}")
        assert r["after"] == settled, (
            f"{name}: emn1 reads {r['after']} after the call, not {settled}")

    # --- the oracle's own calls, as emn2's identity ----------------------
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        token = (await c.apphost.create_token(id2)).token
    async with await astral.connect(n1["endpoint"], token=token) as m:
        for label, coro in (("user.list_expelled", m.user.list_expelled()),
                            ("user.list_siblings", m.user.list_siblings()),
                            ("user.expel(emn3)", m.user.expel(id3))):
            text = await refusal(coro)
            assert DENIED in text, f"{label} as the expelled node: {text!r}"

    # --- emn1's state, read as the User ----------------------------------
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        user_token = (await c.apphost.create_token(user_id)).token
    async with await astral.connect(n1["endpoint"], token=user_token) as u:
        roster = {str(m.identity) for m in await u.user.swarm_status()}
        bans = [str(e.expulsion.subject) for e in await u.user.list_expelled()]
    assert roster == {id1, id3}, f"emn1's roster is {sorted(roster)}, not emn1 and emn3"
    assert bans == [id2], f"emn1 holds bans on {bans}, not on emn2 alone"

    print("oracle: the expelled management node read each swarm call as "
          f"{DENIED!r} on emn1 after it was answered before the ban; emn1's "
          "roster and bans stayed as the expulsion left them")


asyncio.run(main())
