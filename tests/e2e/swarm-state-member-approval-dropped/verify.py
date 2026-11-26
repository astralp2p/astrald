#!/usr/bin/env python3
"""Oracle (criterion 2.6): an Approval signed by a Member is dropped on merge:
never stored or forwarded on any node.

- none of node1, node2 and node3 stores node3's Approval, read live after
  the driver waited past one SwarmState pull; node3 itself refused it too;
- the request it answers is still pending on all three;
- control: node2's Approval, offered by the same route, is stored on all
  three within 60 s and its request reads approved there, so the route
  stores what the check accepts and the drop is the signer's tier
  (astral-docs protocols/user/types/mod.user.approval.md: a member answers
  none).
"""
import asyncio
import time

import astral

from lib import swarmstate as ss
from lib.sessionio import load

NAMES = ["node1", "node2", "node3"]
CONVERGE = 60.0


async def statuses(nodes: dict, req_id: str) -> dict:
    out = {}
    for n in NAMES:
        async with await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]) as c:
            out[n] = await ss.status_of(c, req_id)
    return out


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["member_approval"]

    deadline = time.monotonic() + CONVERGE
    while True:
        stored = {n: set(ss.entries(nodes[n])) for n in NAMES}
        control = await statuses(nodes, f["control_request"])
        if all(f["control_approval"] in stored[n] and control[n] == "approved" for n in NAMES) \
                or time.monotonic() >= deadline:
            break
        await asyncio.sleep(2)
    assert all(f["control_approval"] in stored[n] for n in NAMES), (
        f"control: node2's Approval is not stored everywhere: "
        f"{ {n: f['control_approval'] in stored[n] for n in NAMES} }, pushes {f['control_pushed']}")
    assert all(control[n] == "approved" for n in NAMES), f"control request status {control}"

    holders = [n for n in NAMES if f["member_approval"] in stored[n]]
    assert not holders, f"node3's (a Member's) Approval is stored on {holders}"
    member = await statuses(nodes, f["member_request"])
    assert all(member[n] == "pending" for n in NAMES), (
        f"the request node3 approved is not pending everywhere: {member}")
    print("oracle: the Member's Approval is stored nowhere and its request stays pending; "
          "a Privileged Approval by the same route is stored everywhere")


asyncio.run(main())
