#!/usr/bin/env python3
"""verify expel-node: node1 (the User) permanently banned node2 from the swarm.

Check, independent of run.sh: node2 is recorded in user.list_expelled and gone from node1's
user.swarm_status roster (user.OpSwarmStatus -> ActiveNodes filters the expelledSet). Link state not asserted.
# why: node2's identity comes from node1's siblings.json, not node2 itself -- once expelled, node2 rejects user.info (untokened and under the User token), so it is not a usable identity source
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach sibling tasks/_lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import astralapi  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--node1", default="node1")
    ap.add_argument("--node2", default="node2")
    args, _ = ap.parse_known_args()
    vm1 = args.node1

    # why: list_expelled / swarm_status require the caller to be the contract issuer, so node1 runs under the User token
    info1 = astralapi.home_json(vm1, "user.json")
    U = astralapi.normalize_id(info1.get("user_id", ""))
    token = info1.get("user_token", "")

    # why: node2's identity from node1's siblings.json (recorded by adopt-node) -- the expelled node itself can't be queried post-ban
    sibs = astralapi.home_json(vm1, "siblings.json")
    sib_ids = [astralapi.normalize_id(x) for x in (sibs.get("sibling_ids") or []) if x]
    expelled_id = sib_ids[0] if sib_ids else None

    with astralapi.connect(vm1, token=token) as n1:
        n1_expelled = n1.call("user.list_expelled")
        members = astralapi.swarm_identities(n1.call("user.swarm_status"))

    errs = []
    if not U:
        errs.append("no user_id in node1's user.json")
    if not expelled_id:
        errs.append("no sibling_ids in node1's siblings.json -- can't identify the expelled node")
    if expelled_id and not astralapi.is_expelled(n1_expelled, expelled_id):
        errs.append(f"node2 {expelled_id} is NOT in node1's user.list_expelled "
                    "(expulsion was never issued -- agent did not expel the node)")
    if expelled_id and expelled_id in members:
        errs.append(f"node2 {expelled_id} still appears in node1's user.swarm_status "
                    "(roster not reduced -- expelledSet filter did not drop it)")

    if errs:
        return astralapi.report_errors(errs, "expel-node")

    print(f"expel OK: User {U[:8]}.. banned node2 {expelled_id[:8]}.. -- recorded in "
          f"user.list_expelled and dropped from user.swarm_status "
          f"({len(members)} member(s) remain).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
