#!/usr/bin/env python3
"""Driver: who may call `user.expel`, what it refuses, and a ban made twice.

The driver acts and judges nothing. The User on node1 adopts a fresh node,
`ea-target`, so there is a member to expel without touching node2. Then every
caller that must not expel it tries, and the driver records how each came
back, with node1's roster and ban list read after each try:

- a token minted for a fresh app identity on node1;
- node2's node token on node2: a member, calling its own node;
- the User's identity on node2, which holds no User key: node2 holds the
  contract, so the call is authorized and fails when it signs;
- the User on node1 naming the User itself.

Last, ea-target is expelled twice on node1: first with node1's own node token,
which is the swarm's management node and holds the User's key and the
contract's admin permit, then by the User. The two answers are recorded for the
oracle, which judges them against node1's stored bans.

why a token minted for the User on node2: apphost.create_token hands out a
credential for any identity (astral-py apphost.create_token), and it is how a
caller acts as the User on a node that does not hold the key.
"""
import asyncio

import astral
from astral.api.crypto import public_key_to_identity

from lib.sessionio import load, write_facts

DEADLINE = 20


async def attempt(coro) -> dict:
    """One expel as it came back: answered, or refused with its text."""
    try:
        v = await asyncio.wait_for(coro, DEADLINE)
    except Exception as e:  # noqa: BLE001 — every refusal is an outcome here
        return {"outcome": "refused", "detail": f"{type(e).__name__}: {e}"[:300]}
    return {"outcome": "answered", "detail": str(v)[:120]}


async def view(u) -> dict:
    """node1's roster and ban list as the User reads them."""
    return {"roster": sorted(str(m.identity) for m in await u.user.swarm_status()),
            "bans": sorted(str(e.expulsion.subject)
                           for e in await u.user.list_expelled())}


async def main():
    doc = load()
    n1, n2, t = (doc["nodes"][n] for n in ("node1", "node2", "ea-target"))
    user_id, user_token = doc["facts"]["user_id"], doc["facts"]["user_token"]

    # node1 <-> ea-target only, as swarm-roster-sync does for its node3.
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        await c.nodes.add_endpoint(t["identity"], t["lan_endpoint"], experimental=True)
    async with await astral.connect(t["endpoint"], token=t["token"]) as c:
        await c.nodes.add_endpoint(n1["identity"], n1["lan_endpoint"], experimental=True)
    async with await astral.connect(n1["endpoint"], token=user_token) as u:
        await u.user.adopt(t["identity"])
        baseline = await view(u)

        # a fresh app identity, and a User token on node2 that holds no key
        async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
            app_id = public_key_to_identity(
                await c.crypto.public_key(await c.crypto.new_key()))
            app_token = (await c.apphost.create_token(app_id)).token
        async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
            user_on_node2 = (await c.apphost.create_token(user_id)).token

        callers = {
            "node1_app_token": (n1, app_token, t["identity"]),
            "node2_node_token": (n2, n2["token"], t["identity"]),
            "user_without_key": (n2, user_on_node2, t["identity"]),
            "user_names_user": (n1, user_token, user_id),
        }
        refusals = {}
        for name, (node, token, subject) in callers.items():
            async with await astral.connect(node["endpoint"], token=token) as c:
                r = await attempt(c.user.expel(subject))
            r["after"] = await view(u)
            refusals[name] = r

        async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
            first = await c.user.expel(t["identity"])
        second = await u.user.expel(t["identity"])

    def ban(s):
        return {"subject": str(s.expulsion.subject),
                "issuer": str(s.expulsion.issuer),
                "expelled_at": str(s.expulsion.expelled_at)}

    write_facts({"target_id": t["identity"], "baseline": baseline,
                 "refusals": refusals, "first_ban": ban(first),
                 "second_ban": ban(second)})
    for name, r in refusals.items():
        print(f"driver: {name}: {r['outcome']}: {r['detail']}")
    print(f"driver: ea-target {t['identity'][:16]}… expelled twice; "
          f"first {ban(first)['expelled_at']}, second {ban(second)['expelled_at']}")


asyncio.run(main())
