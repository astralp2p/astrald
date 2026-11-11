#!/usr/bin/env python3
"""Oracle: only the User expels, the swarm user is never banned, a ban made
twice is one ban, and a refused call changes nothing.

Judged from the driver's record and the oracle's own reads:

- the baseline, read before any call: node1's roster is node1, node2 and
  ea-target, and it holds no ban;
- the four refusals, each with its stable text: two callers that are not
  the User ("not permitted"), the User's identity on node2, which holds no
  key ("no signing scheme available"; Expel signs through crypto, so the
  "sign as issuer" prefix of the auth module does not appear), and the User
  named as the subject ("cannot expel the swarm user"); node1's roster and
  bans, read after each, equal the baseline;
- the duplicate: both expels of ea-target, the first by node1's node token
  (the management node), the second by the User, answered a ban by the User
  on ea-target, and node1 stores exactly one, the first one, with its first
  `expelled_at` (a second call signs a new ban and the store keeps the
  old, `db.go StoreExpulsion` DoNothing);
- node1 and node2 end with ea-target out of the roster and that one ban,
  and no other ban, so no refusal banned the User or anyone else;
- the oracle repeats two refusals itself after the ban, so a driver that
  never called does not pass.
"""
import asyncio
import time

import astral

from lib.sessionio import load

CONVERGE = 30.0     # the ban reaches node2 by an asynchronous push
POLL = 1.0

# caller -> the stable text its refusal carries
WANT = {
    "node1_app_token": "not permitted",
    "node2_node_token": "not permitted",
    "user_without_key": "no signing scheme available",
    "user_names_user": "cannot expel the swarm user",
}


def roster(members) -> set:
    return {str(m.identity) for m in members if m.identity is not None}


def bans(expelled) -> dict:
    """subject -> [expelled_at] of every ban in a list_expelled answer."""
    out = {}
    for e in expelled:
        out.setdefault(str(e.expulsion.subject), []).append(str(e.expulsion.expelled_at))
    return out


async def refusal(coro) -> str:
    try:
        v = await asyncio.wait_for(coro, 20)
    except Exception as e:  # noqa: BLE001 — the refusal text is what is judged
        return str(e)
    raise AssertionError(f"answered, not refused: {v!r}")


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    f = doc["facts"]
    id1, id2, target = n1["identity"], n2["identity"], f["target_id"]
    user_id, user_token = f["user_id"], f["user_token"]

    # --- the record: baseline and refusals -------------------------------
    base = f["baseline"]
    assert set(base["roster"]) == {id1, id2, target} and base["bans"] == [], (
        f"baseline: roster {base['roster']}, bans {base['bans']}; wanted node1, "
        "node2 and ea-target with no ban")
    assert set(f["refusals"]) == set(WANT), f"callers recorded: {sorted(f['refusals'])}"
    for name, mark in WANT.items():
        r = f["refusals"][name]
        assert r["outcome"] == "refused", f"{name}: {r['outcome']}: {r['detail']}"
        assert mark in r["detail"], f"{name}: refused with {r['detail']!r}, not {mark!r}"
        assert r["after"] == base, (
            f"{name}: node1 reads roster/bans {r['after']} after the refusal, "
            f"not the baseline {base}")

    # --- the duplicate: two answers, one stored ban ----------------------
    first, second = f["first_ban"], f["second_ban"]
    for label, b in (("first", first), ("second", second)):
        assert (b["subject"], b["issuer"]) == (target, user_id), (
            f"the {label} expel answered {b}, not the User's ban on ea-target")

    # --- the oracle's own refusals, after the ban ------------------------
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        text = await refusal(c.user.expel(target))
    assert "not permitted" in text, f"node2's node token expelling: {text!r}"
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        text = await refusal(c.user.expel(user_id))
    assert "cannot expel the swarm user" in text, f"the User expelling the User: {text!r}"

    # --- node1 and node2: the settled state ------------------------------
    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        b1, r1 = bans(await c.user.list_expelled()), roster(await c.user.swarm_status())
    assert b1 == {target: [first["expelled_at"]]}, (
        f"node1 holds {b1}; wanted one ban on ea-target at {first['expelled_at']} "
        f"(the second call signed {second['expelled_at']})")
    assert r1 == {id1, id2}, f"node1's roster is {sorted(r1)}, not node1 and node2"

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        async def view2():
            return bans(await c.user.list_expelled()), roster(await c.user.swarm_status())
        deadline = time.monotonic() + CONVERGE
        b2, r2 = await view2()
        while (target not in b2 or target in r2) and time.monotonic() < deadline:
            await asyncio.sleep(POLL)
            b2, r2 = await view2()
    assert list(b2) == [target] and len(b2[target]) == 1, (
        f"node2 holds {b2} {CONVERGE:.0f} s after the ban; wanted one ban on ea-target")
    assert target not in r2 and {id1, id2} <= r2, f"node2's roster is {sorted(r2)}"

    print(f"oracle: {len(WANT)} refusals read as their texts and left node1's "
          "roster and bans at the baseline; two expels of ea-target stored one "
          "ban; node1 and node2 hold it and no other")


asyncio.run(main())
