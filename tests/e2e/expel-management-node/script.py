#!/usr/bin/env python3
"""Driver: a Privileged node of the User is expelled, and acts on node1 anyway.

emn1 and emn2 are claimed for one User, from the same new words, so each holds
a Privileged membership the user key signed (`user.bootstrap`). emn2 offers
its membership to emn1's SwarmState (`nodes.swarm_push`), so emn1's swarm
holds emn2 as a Privileged node. emn3 joins through emn1 as a Member, the User
approving its JoinRequest.

Controls first: with a token minted on emn1 for emn2's identity, the reads
that the report names are allowed while emn2 is a member:
`user.list_siblings` and `user.list_expelled`. `user.expel` naming the User is
refused even now: only the user expels (D7), and a Privileged membership does
not cover it.

Then the User on emn1 expels emn2, and the same three calls are made with the
same token, plus `user.expel` naming emn3. The token is local to emn1, so no
ban answer from emn2 can close a link under the calls and change their
outcome. emn1's roster and ban list are read after each.

The driver judges nothing; the oracle reads emn1's state itself.

why a token minted for emn2's identity on emn1: apphost.create_token hands
out a credential for any identity (expel-authority does the same), and a
caller with it is emn2 as far as emn1's authorizers can tell.
"""
import asyncio

import astral
from astral.api.bip137sig import mnemonic_local, new_entropy_local

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

DEADLINE = 20


async def attempt(coro) -> dict:
    """One call as it came back: answered, or refused with its text."""
    try:
        v = await asyncio.wait_for(coro, DEADLINE)
    except Exception as e:  # noqa: BLE001 — every refusal is an outcome here
        return {"outcome": "refused", "detail": f"{type(e).__name__}: {e}"[:300]}
    return {"outcome": "answered", "detail": str(v)[:120]}


async def view(u) -> dict:
    """emn1's roster and ban list as the User reads them."""
    return {"roster": sorted(str(m.identity) for m in await u.user.swarm_status()),
            "bans": sorted(str(e.expulsion.subject)
                           for e in await u.user.list_expelled())}


async def link(a: dict, b: dict) -> None:
    """a learns b's address and dials it, as itself; b learns a's too."""
    async with await astral.connect(a["endpoint"], token=a["token"]) as c:
        await c.nodes.add_endpoint(b["identity"], b["lan_endpoint"], experimental=True)
        await c.nodes.new_link(b["identity"], endpoint=b["lan_endpoint"],
                               strategies="basic", experimental=True,
                               timeout=60.0)
    async with await astral.connect(b["endpoint"], token=b["token"]) as c:
        await c.nodes.add_endpoint(a["identity"], a["lan_endpoint"], experimental=True)


async def swarm_has(u, identity: str, tries: int = 30) -> bool:
    for _ in range(tries):
        if identity in (await view(u))["roster"]:
            return True
        await asyncio.sleep(1)
    return False


async def calls(c, member: str, user_id: str) -> dict:
    """The calls of the report, as the session c, and a roster-changing one."""
    return {
        "list_expelled": await attempt(c.user.list_expelled()),
        "list_siblings": await attempt(c.user.list_siblings()),
        "expel_user": await attempt(c.user.expel(user_id)),
    }


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("emn1", "emn2", "emn3"))

    words = mnemonic_local(new_entropy_local(128))
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        boot1 = await c.user.bootstrap(words)
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        boot2 = await c.user.bootstrap(words)
    user_id, user_token = str(boot1.identity), str(boot1.token)
    assert str(boot2.identity) == user_id, "the same words made two Users"

    # emn1 <-> emn2: emn2 offers its own membership to emn1's SwarmState
    await link(n1, n2)
    pushed = ss.offer(n2, ss.membership_id(n2, n2["identity"]), [n1])
    assert pushed[n1["identity"]] == "ack", f"emn1 refused emn2's membership: {pushed}"

    await ss.admit(n1, user_token, n3)
    async with await astral.connect(n1["endpoint"], token=user_token) as u:
        assert await swarm_has(u, n2["identity"]), "emn2 never joined emn1's swarm"
        assert await swarm_has(u, n3["identity"]), "emn3 never joined emn1's swarm"
        baseline = await view(u)

        async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
            member_token = (await c.apphost.create_token(n2["identity"])).token

        async with await astral.connect(n1["endpoint"], token=member_token) as m:
            controls = await calls(m, n2["identity"], user_id)

            await u.user.expel(n2["identity"])
            expelled = await view(u)

            refusals = {}
            probes = await calls(m, n2["identity"], user_id)
            probes["expel_member"] = await attempt(m.user.expel(n3["identity"]))
            for name, r in probes.items():
                r["after"] = await view(u)
                refusals[name] = r

    write_facts({"user_id": user_id, "controls": controls, "refusals": refusals,
                 "baseline": baseline, "banned_id": n2["identity"],
                 "member_id": n3["identity"], "expelled": expelled})
    for name, r in {**{f"control {k}": v for k, v in controls.items()}, **refusals}.items():
        print(f"driver: {name}: {r['outcome']}: {r['detail']}")


asyncio.run(main())
