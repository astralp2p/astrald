#!/usr/bin/env python3
"""verify expel-node: node1 (the User) permanently banned node2 from the swarm.

Independent check (does not trust run.sh). Asserts node2 is recorded in
user.list_expelled and is gone from node1's user.swarm_status roster
(user.OpSwarmStatus -> ActiveNodes filters the expelledSet). Link state is not
asserted.

node2's identity comes from node1's siblings.json (recorded by adopt-node), NOT
from node2 itself: once expelled, node2 rejects user.info (query rejected (2)
untokened, auth_failed with the User token -- it no longer accepts the User it
was banned from), so it is not a usable identity source.

Queries reach node1's apphost through the shared astral-py client
(tasks/_lib/astralapi.py), CLI fallback for anything it can't serve.
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling tasks/_lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import astralapi  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--node1", default="node1")
    ap.add_argument("--node2", default="node2")
    args, _ = ap.parse_known_args()
    vm1 = args.node1

    # node1 acts as the User (token from bootstrap); list_expelled / swarm_status
    # require the caller to be the contract issuer, so they run under that token.
    info1 = astralapi.home_json(vm1, "user.json")
    U = "".join(str(info1.get("user_id", "")).split())
    token = info1.get("user_token", "")

    # node2's identity from node1's siblings.json (recorded by adopt-node) -- a
    # stable source. The expelled node itself can't be queried (post-ban node2
    # rejects user.info).
    sibs = astralapi.home_json(vm1, "siblings.json")
    sib_ids = ["".join(str(x).split()) for x in (sibs.get("sibling_ids") or []) if x]
    expelled_id = sib_ids[0] if sib_ids else None

    with astralapi.connect(vm1, token=token) as n1:
        bans = n1.expulsions()
        members = {m.identity for m in n1.swarm_members() if m.identity}

    errs = []
    if not U:
        errs.append("no user_id in node1's user.json")
    if not expelled_id:
        errs.append("no sibling_ids in node1's siblings.json -- can't identify the expelled node")
    if expelled_id and not any(b.bans(expelled_id) for b in bans):
        errs.append(f"node2 {expelled_id} is NOT in node1's user.list_expelled "
                    "(expulsion was never issued -- agent did not expel the node)")
    if expelled_id and expelled_id in members:
        errs.append(f"node2 {expelled_id} still appears in node1's user.swarm_status "
                    "(roster not reduced -- expelledSet filter did not drop it)")

    if errs:
        sys.stderr.write("expel-node verify FAILED:\n")
        for e in errs:
            sys.stderr.write(f"  - {e}\n")
        return 1

    print(f"expel OK: User {U[:8]}.. banned node2 {expelled_id[:8]}.. -- recorded in "
          f"user.list_expelled and dropped from user.swarm_status "
          f"({len(members)} member(s) remain).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
