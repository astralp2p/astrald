#!/usr/bin/env python3
"""Driver (criterion 2.4): an app reaching a service on its own node, the
Member node3, is allowed by its contract chain, and denied without it.

1. A wallet stand-in registers on node3; the User approves its request on
   node1 and the wallet (lib/apps, long-lived) advertises `wallet` as
   private on node3.
2. Two apps register on node3, each asking a ServiceDiscovery permit for
   `wallet`: `front` and `other`. Once the User sees the wallet, each app's
   local discovery (reach=local, node3 alone) is recorded, before any answer.
3. The User approves front's request and declines other's, on node1. The
   driver polls front's local discovery until it shows the wallet (up to
   60 s), then records other's, and front spends 50.

Records `app_own_node`: {wallet, front, other, user_saw_s, before_front,
before_other, front_request, other_request, front_seen_after_s, other_after,
spend_50}.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

WAIT = 60.0


async def pending_for(cu, identity: str) -> ss.Request:
    return await ss.find(cu, lambda r: r.type == ss.CONTRACT and r.names(identity)
                         and r.status == "pending")


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    user_token = doc["facts"]["user_token"]
    f = {}
    async with await astral.connect(n1["endpoint"], token=user_token) as cu:
        wallet = ss.register(n3)
        await ss.answer(cu, (await pending_for(cu, wallet["identity"])).id, "approve")
        ss.spawn_wallet("swarm-state-wallet-node3", n3, wallet["token"])
        f["wallet"] = wallet

        f["front"], f["other"] = ss.register(n3, discovery=True), ss.register(n3, discovery=True)
        f["user_saw_s"] = await ss.await_discovery(n1, user_token, "swarm",
                                                   wallet["identity"], WAIT)
        assert f["user_saw_s"] is not None, "the User never saw the wallet"
        f["before_front"] = ss.discover(n3, f["front"]["token"], "local")
        f["before_other"] = ss.discover(n3, f["other"]["token"], "local")

        front_req = await pending_for(cu, f["front"]["identity"])
        other_req = await pending_for(cu, f["other"]["identity"])
        await ss.answer(cu, front_req.id, "approve")
        await ss.answer(cu, other_req.id, "decline")
        f["front_request"], f["other_request"] = front_req.id, other_req.id

    f["front_seen_after_s"] = await ss.await_discovery(
        n3, f["front"]["token"], "local", wallet["identity"], WAIT)
    f["other_after"] = ss.discover(n3, f["other"]["token"], "local")
    f["spend_50"] = ss.spend(n3, f["front"]["token"], wallet["identity"], 50)
    write_facts({"app_own_node": f})
    print(f"driver: front saw the wallet on its own node {f['front_seen_after_s']} s "
          "after its Approval")


asyncio.run(main())
