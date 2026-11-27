#!/usr/bin/env python3
"""Oracle (criterion 2.3): an app moved to another device gets its access
back through a re-issued contract.

- on arrival at node2, before any new request, the front sees no wallet: its
  old chain through the expelled node3 no longer holds;
- the request the front filed on node2 is a fresh one: another entry ID than
  its request on node3, filed by node2 for the front, fulfilled;
- apphost.request_contract returned the contract node2 signed for the front,
  naming that fresh request as its RequestID (astral-docs
  protocols/apphost/ops/apphost.request_contract.md);
- within 60 s the front's local discovery on node2 shows the wallet, and
  still does at the oracle (read live); the wallet spends 50 for the front.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    f = doc["facts"]["app_moves"]
    front, wallet = f["front"], f["wallet"]

    assert wallet not in f["before"], "the front saw the wallet on node2 before a fresh request"
    assert f["request_id"] != f["old_request"], "the front's request on node2 is its old one"
    req = ss.entry_on(n2, f["request_id"])
    assert req, f"node2 does not store the request {f['request_id']}"
    body = req["ContractRequest"]
    assert body["FiledBy"] == n2["identity"] and body["Identity"] == front, (
        f"the fresh request is not node2's for the front: {body}")
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        status = await ss.status_of(cu, f["request_id"])
    assert status == "fulfilled", f"the fresh request is {status}"

    c = f["contract"]
    assert c and c["issuer"] == n2["identity"] and c["subject"] == front, (
        f"apphost.request_contract answered {f['request_answer']}")
    assert c.get("request_id") == f["request_id"], (
        f"the re-issued contract names {c.get('request_id')}, not the fresh request")

    assert f["seen_after_s"] is not None, "the front never saw the wallet again within 60 s"
    assert wallet in ss.discover(n2, f["token"], "local"), (
        "at the oracle, the moved front sees no wallet")
    assert "answer" in f["spend_50"] and front in f["spend_50"]["answer"], (
        f"the wallet did not spend 50 for the moved front: {f['spend_50']}")
    print(f"oracle: the front moved to node2 regained the wallet through node2's "
          f"re-issued contract ({f['seen_after_s']:.1f} s)")


asyncio.run(main())
