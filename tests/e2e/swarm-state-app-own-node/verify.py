#!/usr/bin/env python3
"""Oracle (criterion 2.4): an app reaching a service on its own node is
allowed by its contract chain, and denied without it.

- before any answer, neither app's local discovery on node3 shows the
  private wallet, while the User sees it;
- after the User approves front's request, front's local discovery shows the
  wallet within 60 s, and still does at the oracle (read live), and the
  wallet spends 50 for front;
- node3 stores the contract that carries front's Approval out: issued by
  node3 for front, with an ApprovalID naming an Approval of front's request;
- other, whose request the User declined, still sees no wallet, at the
  driver and at the oracle, and node3 lists its request as declined.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load


async def main():
    doc = load()
    n3 = doc["nodes"]["node3"]
    f = doc["facts"]["app_own_node"]
    wallet, front = f["wallet"]["identity"], f["front"]["identity"]

    for name in ("before_front", "before_other"):
        assert wallet not in f[name], f"{name}: an app saw the wallet before any answer"
    assert f["front_seen_after_s"] is not None, (
        "front's local discovery never showed the wallet within 60 s of its Approval")
    assert wallet in ss.discover(n3, f["front"]["token"], "local"), (
        "at the oracle, front's local discovery shows no wallet")
    assert "answer" in f["spend_50"] and front in f["spend_50"]["answer"], (
        f"the wallet did not spend 50 for front: {f['spend_50']}")

    entries = ss.entries(n3)
    linked = [e["Object"]["Contract"] for e in entries.values()
              if e["Type"] == "mod.auth.signed_contract"
              and e["Object"]["Contract"].get("Subject") == front
              and e["Object"]["Contract"].get("Issuer") == n3["identity"]
              and (entries.get(e["Object"]["Contract"].get("ApprovalID") or "") or {})
              .get("Object", {}).get("Approval", {}).get("RequestID") == f["front_request"]]
    assert linked, "node3 stores no contract for front carrying an Approval of its request"

    assert wallet not in f["other_after"], "the declined app saw the wallet"
    assert wallet not in ss.discover(n3, f["other"]["token"], "local"), (
        "at the oracle, the declined app sees the wallet")
    async with await astral.connect(n3["endpoint"], token=n3["token"]) as c3:
        status = await ss.status_of(c3, f["other_request"])
    assert status == "declined", f"node3 lists the declined app's request as {status}"
    print(f"oracle: front reached the wallet on its own node by its chain "
          f"({f['front_seen_after_s']:.1f} s); the declined app did not")


asyncio.run(main())
