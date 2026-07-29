#!/usr/bin/env python3
"""verify object-store: the stored object is present in the holder's local repo.

Repo-pinned, ungated objects.load -repo local on the holder must return the exact
stored bytes. Holder resolves from --target: localnode/node1 -> node1, node2 -> node2.
# why: verify reads the bytes back itself; the agent only stores and records the id.
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
    ap.add_argument("--vm", default="node1")          # operator; records object.json
    ap.add_argument("--node2", default="node2")       # peer
    ap.add_argument("--target", default="localnode")  # localnode/node1 -> node1; node2 -> node2
    args, _ = ap.parse_known_args()
    holder = args.node2 if args.target == args.node2 else args.vm

    ID = astralapi.normalize_id(astralapi.home_json(args.vm, "object.json").get("object_id", ""))
    # note: ground-truth bytes -- the fixed payload.txt run.sh shipped to the operator,
    #       not the agent's account of what it stored.
    PAY = astralapi.read_file(args.vm, "/home/tester/payload.txt")

    # decisive: re-load from the holder's local repo (repo-pinned + ungated) and match payload.txt
    with astralapi.connect(holder) as h:
        h_load = h.call("objects.load", {"id": ID, "repo": "local"})
    got = astralapi.loaded_payload(h_load)
    local_ok = got is not None and got.rstrip("\n") == PAY

    errs = []
    if not ID:
        errs.append("no object_id in node1's object.json")
    if not PAY:
        errs.append("payload.txt missing on the operator (run.sh must ship it)")

    if not errs and local_ok:
        print(f"object-store OK (target={args.target}): {holder}'s local repo holds object "
              f"{ID[:12]}.. with the exact bytes ({len(PAY)} B).")
        return 0

    sys.stderr.write(f"object-store verify FAILED (target={args.target}): {holder}'s local repo "
                     "does NOT hold the stored object.\n")
    for e in errs:
        sys.stderr.write(f"  - {e}\n")
    if got is None:
        sys.stderr.write(f"  objects.load -repo local on {holder} returned no payload (see errors below).\n")
    elif not local_ok:
        sys.stderr.write(f"  bytes mismatch: got {got!r} != stored {PAY!r}.\n")
    for e in astralapi.error_messages(h_load):
        sys.stderr.write(f"  load error_message: {e}\n")
    sys.stderr.write(f"  (id={ID} holder={holder} load={'hit' if got is not None else 'miss'})\n")
    return 1


if __name__ == "__main__":
    sys.exit(main())
