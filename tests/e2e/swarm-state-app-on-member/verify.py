#!/usr/bin/env python3
"""Oracle (criterion 2.2): an app on a Member node reaches the wallet only
after the User approves on a Privileged node, and the wallet still decides
its limits.

- before the Approval, a swarm discovery by the front app shows no wallet
  (the wallet is private: only a caller holding a discovery permit sees it,
  astral-docs protocols/services/ops/services.discover.md, Visibility);
- node1, a Privileged node, signed the Approval, and the request is approved
  on node1 and on node3;
- within 60 s of the Approval the front's swarm discovery shows the wallet,
  and it still does at the oracle (read live);
- node3 holds the contract that carries the Approval out: issued by node3
  for the front, with that Approval's ID;
- the wallet spends 50 for the front and refuses 500 with its own error.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load


async def main():
    doc = load()
    n1, n3 = doc["nodes"]["node1"], doc["nodes"]["node3"]
    f = doc["facts"]["app_on_member"]
    wallet, front = f["wallet"]["identity"], f["front"]["identity"]

    assert wallet not in f["before"], (
        f"the front saw the wallet before any Approval: {f['before']}")
    assert f["seen_after_s"] is not None, (
        "the front's swarm discovery never showed the wallet within 60 s of the Approval")
    assert wallet in ss.discover(n3, f["front"]["token"], "swarm"), (
        "at the oracle, the front's swarm discovery shows no wallet")

    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        status = await ss.status_of(cu, f["request_id"])
    assert status == "approved", f"node1 lists the front's request as {status}"
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3:
        status = await ss.status_of(c3, f["request_id"])
    contracts = ss.contracts_of(n3, front)
    assert status == "approved", f"node3 lists the front's request as {status}"
    approvals = [c for c in contracts if c.get("ApprovalID")]
    assert any(c["Issuer"] == n3["identity"] for c in approvals), (
        f"node3 holds no contract it issued for the front with an ApprovalID: {contracts}")
    approval_id = approvals[0]["ApprovalID"]
    approval = ss.entry_on(n1, approval_id)
    assert approval and approval["Approval"]["Issuer"] == f["approver"] and \
        approval["Approval"]["RequestID"] == f["request_id"], (
        f"the contract's Approval {approval_id} is not node1's of {f['request_id']}: {approval}")

    assert "answer" in f["spend_50"] and front in f["spend_50"]["answer"], (
        f"the wallet did not spend 50 for the front: {f['spend_50']}")
    assert "over limit" in f["spend_500"].get("error", ""), (
        f"the wallet did not refuse 500 itself: {f['spend_500']}")
    print(f"oracle: front on Member node3 reached the wallet {f['seen_after_s']:.1f} s "
          "after node1's Approval; the wallet refused 500 itself")


asyncio.run(main())
