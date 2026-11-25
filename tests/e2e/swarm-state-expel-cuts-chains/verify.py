#!/usr/bin/env python3
"""Oracle (criterion 2.7): expelling the Member node cuts every app chain
through it.

- before the expel, the front's discovery showed the wallet on node3, on
  node2 and from node1: its chain held on every node;
- node1 and node2 store the Expulsion of node3;
- within 60 s of the expel, and again at the oracle (read live), the front's
  discovery on node2 and from node1 shows no wallet;
- node1 and node2 still store the contract node3 issued for the front: the
  contract is there, and the expel of its issuer is what cuts the chain
  (astral-docs protocols/user/types/mod.user.request_status.md and the
  Approval link: an expel of the issuer, subject or approver vetoes it).
"""
import asyncio

from lib import swarmstate as ss
from lib.sessionio import load


async def main():
    doc = load()
    nodes, f = doc["nodes"], doc["facts"]["expel_cuts"]
    n1, n2, n3 = nodes["node1"], nodes["node2"], nodes["node3"]
    wallet, front = f["wallet"], f["front"]

    missing = [n for n, v in f["before"].items() if wallet not in v]
    assert not missing, f"before the expel, the front's chain did not hold on {missing}"
    assert all(f["expulsion_on"].values()), f"Expulsion stored: {f['expulsion_on']}"
    assert f["cut_after_s"] is not None, (
        f"60 s after the expel the front still saw the wallet: {f.get('after')}")
    live = {"node2": ss.discover(n2, f["tokens"]["node2"], "local"),
            "node1": ss.discover(n1, f["tokens"]["node1"], "swarm")}
    assert not any(wallet in v for v in live.values()), (
        f"at the oracle, the front's chain through node3 still holds: {live}")
    for name, n in (("node1", n1), ("node2", n2)):
        issued = [c for c in ss.contracts_of(n, front) if c["Issuer"] == n3["identity"]
                  and c.get("ApprovalID")]
        assert issued, f"{name} no longer stores node3's contract for the front"
    print(f"oracle: expelling node3 cut the front's chain on node1 and node2 "
          f"({f['cut_after_s']:.1f} s); node3's contract is still stored")


asyncio.run(main())
