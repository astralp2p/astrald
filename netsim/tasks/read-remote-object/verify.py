#!/usr/bin/env python3
"""verify read-remote-object: node1 read the peer's object over astral.

Re-read the peer's object as the User via <peer>:objects.load and match the stored payload.
# why: read as the User (node1 holds the token) -- authenticated, so the query keeps the
#      network zone and routes to the peer.
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
    ap.add_argument("--vm", default="node1")      # operator; reads as the User
    ap.add_argument("--peer", default="node2")    # node holding the object (alias)
    args, _ = ap.parse_known_args()

    obj = astralapi.home_json(args.vm, "object.json")    # object-store: object_id
    user = astralapi.home_json(args.vm, "user.json")     # bootstrap/import: user_token
    rd = astralapi.home_json(args.vm, "read.json")       # this task's agent: object_remote
    ID = astralapi.normalize_id(obj.get("object_id", ""))
    # note: ground-truth bytes -- the fixed payload.txt object-store shipped to the operator,
    #       not the agent's account of what was stored.
    PAY = astralapi.read_file(args.vm, "/home/tester/payload.txt")
    REMOTE = str(rd.get("object_remote", ""))
    token = user.get("user_token", "")

    # decisive: node1, as the User, reads the peer's object over astral
    with astralapi.connect(args.vm, token=token) as n1:
        out = n1.call("objects.load", {"id": ID}, target=args.peer)
    got = astralapi.loaded_payload(out)
    read_ok = got is not None and got.rstrip("\n") == PAY

    errs, notes = [], []
    if not ID:
        errs.append("no object_id in node1's object.json (object-store --target node2 must run first)")
    if not PAY:
        errs.append("payload.txt missing on node1 (object-store --target node2 must run first)")
    if not token:
        errs.append("no user_token in node1's user.json (can't read the peer as the User)")
    if not REMOTE:
        notes.append("agent recorded no object_remote (the agent's own read)")
    elif PAY and PAY not in REMOTE:
        notes.append(f"agent's recorded read does not contain the payload ({REMOTE!r})")

    if not errs and read_ok:
        print(f"read-remote-object OK: node1 (as User) read object {ID[:12]}.. from "
              f"{args.peer} over astral; bytes match ({len(PAY)} B).")
        for n in notes:
            sys.stderr.write(f"  note: {n}\n")
        return 0

    sys.stderr.write(f"read-remote-object verify FAILED: node1 could not read the object from "
                     f"{args.peer} over astral.\n")
    for e in errs:
        sys.stderr.write(f"  - {e}\n")
    if got is None:
        sys.stderr.write(f"  {args.peer}:objects.load (as User) returned no payload "
                         "(route_not_found means the read didn't route -- check auth/zone).\n")
    elif not read_ok:
        sys.stderr.write(f"  bytes mismatch: got {got!r} != stored {PAY!r}.\n")
    for e in astralapi.error_messages(out):
        sys.stderr.write(f"  load error_message: {e}\n")
    for n in notes:
        sys.stderr.write(f"  note: {n}\n")
    sys.stderr.write(f"  (id={ID} peer={args.peer} read={'hit' if got is not None else 'miss'})\n")
    return 1


if __name__ == "__main__":
    sys.exit(main())
