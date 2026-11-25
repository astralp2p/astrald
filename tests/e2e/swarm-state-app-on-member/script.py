#!/usr/bin/env python3
"""Driver (criterion 2.2): an app on the Member node3 reaches the wallet on
node2 once the User approves its ContractRequest on node1, a Privileged node;
the wallet's own handler still refuses what is over its limit.

1. The wallet stand-in registers on node2. Its registration files a
   ContractRequest for the hosting permits; the User approves it on node1, and
   the wallet (lib/apps, long-lived) advertises `wallet` as private.
2. The front app registers on node3, asking a ServiceDiscovery permit for
   `wallet`. Once the User, on node1, sees the wallet in a swarm discovery,
   the front's own swarm discovery is recorded, before any answer.
3. The User approves the front's request on node1. The driver polls the
   front's swarm discovery until it shows the wallet (up to 60 s), then the
   front spends 50 (within the limit) and 500 (over it).

Records `app_on_member`: {wallet, front, user_saw_s, request_id, approver, before,
seen_after_s, spend_50, spend_500}.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

WAIT = 60.0


async def approve_for(cu, identity: str) -> ss.Request:
    """The User approves the pending ContractRequest naming identity."""
    r = await ss.find(cu, lambda r: r.type == ss.CONTRACT and r.names(identity)
                      and r.status == "pending")
    await ss.answer(cu, r.id, "approve")
    return r


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    facts = {}
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        wallet = ss.register(n2)
        await approve_for(cu, wallet["identity"])
        ss.spawn_wallet("swarm-state-wallet", n2, wallet["token"])
        facts["wallet"] = wallet

        front = ss.register(n3, discovery=True)
        facts["front"] = front
        # why the User looks first: an empty discovery by the front says
        # something only once the wallet's offering is live, and the User
        # sees every offering.
        facts["user_saw_s"] = await ss.await_discovery(
            n1, doc["facts"]["user_token"], "swarm", wallet["identity"], WAIT)
        assert facts["user_saw_s"] is not None, "the User never saw the wallet"
        facts["before"] = ss.discover(n3, front["token"], "swarm")

        req = await approve_for(cu, front["identity"])
        facts["request_id"] = req.id
        facts["approver"] = n1["identity"]

    facts["seen_after_s"] = await ss.await_discovery(
        n3, front["token"], "swarm", wallet["identity"], WAIT)
    facts["spend_50"] = ss.spend(n3, front["token"], wallet["identity"], 50)
    facts["spend_500"] = ss.spend(n3, front["token"], wallet["identity"], 500)
    write_facts({"app_on_member": facts})
    print(f"driver: front on node3 saw the wallet {facts['seen_after_s']} s after the approval")


asyncio.run(main())
