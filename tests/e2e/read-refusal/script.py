#!/usr/bin/env python3
"""Driver: provision a stranger on node1, and a decoy object for it to delete.

The stranger is a fresh guest identity from `apphost.register` — the node mints
the key, signs a contract for it and hands back a token. It is a member of
nothing: not the User, not a node of the local swarm, which is exactly the
identity the read authorizer is supposed to refuse.

The decoy exists so the oracle can probe `objects.delete` without aiming it at
the state's own object. A stranger that can delete would otherwise destroy
`two-nodes-data` on its way to proving it should not have been able to.
"""
import asyncio

import astral

from lib.sessionio import load, write_facts

DECOY = b"read-refusal decoy: the stranger must not be able to delete this\n"


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    user_token = doc["facts"]["user_token"]

    async with await astral.connect(n1["endpoint"], token=user_token) as c:
        guest = await c.apphost.register()

        # why: the stranger's delete probe needs its own target. objects.store
        # refuses an untyped blob, so raw bytes go through create + commit —
        # the same path object-store takes.
        async with c.objects.create() as w:
            await w.write(DECOY)
            decoy_id = str(await w.commit())

    guest_identity = str(guest.identity) if guest.identity else ""
    assert guest.token, "apphost.register answered no token"
    assert guest_identity, "apphost.register answered no identity"

    write_facts({
        "guest_token": guest.token,
        "guest_identity": guest_identity,
        "decoy_id": decoy_id,
    })
    print(f"driver: registered stranger {guest_identity[:16]}… on node1; "
          f"decoy {decoy_id[:16]}… stored as the User")


asyncio.run(main())
