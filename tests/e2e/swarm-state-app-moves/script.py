#!/usr/bin/env python3
"""Driver (criterion 2.3): an app moved to another device gets its access
back through a re-issued contract, after a fresh request there.

The world holds the front app of swarm-state-app-on-member, whose device,
node3, swarm-state-expel-cuts-chains expelled: its contract from node3 no
longer carries it. The front moves to node2, as a backup restored there: its
key is copied from node3's object store (lib/nodefs) to node2 (objects.store
with node2's token; node2's crypto module indexes it), node2 mints a token
for the front's identity (apphost.create_token with node2's own token), and
the front calls apphost.request_contract there with the same permits it
asked on node3.

1. Before the request, the front's local discovery on node2 shows no wallet.
2. The front's apphost.request_contract on node2 files a fresh
   ContractRequest; the User approves it on node1. The call returns the
   contract node2 signed for the front.
3. The driver polls the front's local discovery on node2 until it shows the
   wallet (up to 60 s), and the front spends 50.

Records `app_moves`: {front, wallet, old_request, before, request_id,
contract, seen_after_s, spend_50}.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

WAIT = 60.0


async def approve(cu, front: str, old: str) -> str:
    r = await ss.find(cu, lambda r: r.type == ss.CONTRACT and r.names(front)
                      and r.status == "pending" and r.id != old, timeout=WAIT)
    await ss.answer(cu, r.id, "approve")
    return r.id


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    prior = doc["facts"]["app_on_member"]
    front, wallet = prior["front"]["identity"], prior["wallet"]["identity"]
    n3 = doc["nodes"]["node3"]
    ss.move_key(n3, n2, front)
    out = ss.app_tool("token", n2["endpoint"], n2["token"], front)
    assert "error" not in out, f"apphost.create_token on node2: {out}"
    token = out["token"]
    f = {"front": front, "wallet": wallet, "old_request": prior["request_id"],
         "before": ss.discover(n2, token, "local")}

    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        loop = asyncio.get_running_loop()
        asked = loop.run_in_executor(None, lambda: ss.app_tool(
            "request", n2["endpoint"], token, int(WAIT), "discovery", timeout=WAIT + 30))
        f["request_id"] = await approve(cu, front, prior["request_id"])
        result = await asked
    f["contract"] = result.get("contract")
    f["request_answer"] = result
    f["seen_after_s"] = await ss.await_discovery(n2, token, "local", wallet, WAIT)
    f["spend_50"] = ss.spend(n2, token, wallet, 50)
    f["token"] = token
    write_facts({"app_moves": f})
    print(f"driver: the front moved to node2; access back {f['seen_after_s']} s after "
          "the fresh Approval")


asyncio.run(main())
