#!/usr/bin/env python3
"""Driver (criterion 2.5): two Privileged nodes answer the same request
differently.

An app registers on the Member node3, which files its ContractRequest. Once
node1 and node2 both list it as pending, the User answers it on both at
once: user.answer_request with `approve` on node1, and with `decline` on node2
(the same op, routed through node1 to node2, so node2 signs the Decline).
Both answers are signed: a Decline is open to a pending or an approved
request (astral-docs protocols/user/ops/user.answer_request.md).

Records `conflicting_answers`: {app, request_id, approval_issuer,
decline_issuer}.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    app = ss.register(n3)
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        req = await ss.find(cu, lambda r: r.type == ss.CONTRACT and r.names(app["identity"]))
        await ss.find(cu, lambda r: r.id == req.id and r.status == "pending",
                      target=n2["identity"])
        approval, decline = await asyncio.gather(
            ss.answer(cu, req.id, "approve"),
            ss.answer(cu, req.id, "decline", target=n2["identity"]))

    write_facts({"conflicting_answers": {
        "app": app["identity"], "request_id": req.id,
        "approval_issuer": approval["Object"]["Approval"]["Issuer"],
        "decline_issuer": decline["Object"]["Decline"]["Issuer"],
    }})
    print(f"driver: node1 approved and node2 declined request {req.id}")


asyncio.run(main())
