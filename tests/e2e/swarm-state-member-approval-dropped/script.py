#!/usr/bin/env python3
"""Driver (criterion 2.6): an Approval signed by a Member is dropped on merge.

Two apps register on node3, which files a ContractRequest for each. Once
node1 and node2 list both, the driver signs an Approval with each of two
nodes' own keys (read from the node's data/node_key with lib/nodefs) and
offers it with nodes.swarm_push to that node's SwarmState and to the other
two nodes:

- the Member node3 signs an Approval of the first app's request;
- the Privileged node2 signs an Approval of the second app's request, by the
  same route: the control that the route stores what the check accepts.

user.answer_request would refuse node3's answer outside its tier and write
nothing, and crypto.sign_hash refuses the node key, so an offered entry is
how a Member's Approval meets a merge. The
driver then waits 20 s, longer than one SwarmState pull (15 s), so a stored
entry has had its chance to spread.

Records `member_approval`: {member_request, member_approval, member_pushed,
control_request, control_approval, control_pushed}.
"""
import asyncio

import astral

from lib import swarmstate as ss
from lib.sessionio import load, write_facts

# why: one SwarmState pull interval (mod/nodes/src/swarm_wire.go, 15 s) and slack
SPREAD = 20.0


async def main():
    doc = load()
    n1, n2, n3 = (doc["nodes"][n] for n in ("node1", "node2", "node3"))
    apps = [ss.register(n3), ss.register(n3)]
    async with await astral.connect(n1["endpoint"], token=doc["facts"]["user_token"]) as cu:
        reqs = [await ss.find(cu, lambda r, a=a: r.type == ss.CONTRACT and r.names(a["identity"])
                              and r.status == "pending") for a in apps]
        for r in reqs:
            await ss.find(cu, lambda x, r=r: x.id == r.id and x.status == "pending",
                          target=n2["identity"])

    member = ss.sign_approval(n3, reqs[0].id, [n1, n2])
    control = ss.sign_approval(n2, reqs[1].id, [n1, n3])
    await asyncio.sleep(SPREAD)

    write_facts({"member_approval": {
        "member_request": reqs[0].id, "member_approval": member["approval_id"],
        "member_pushed": member["pushed"],
        "control_request": reqs[1].id, "control_approval": control["approval_id"],
        "control_pushed": control["pushed"],
    }})
    print(f"driver: node3 offered Approval {member['approval_id']} ({member['pushed']}); "
          f"node2 offered {control['approval_id']} ({control['pushed']})")


asyncio.run(main())
