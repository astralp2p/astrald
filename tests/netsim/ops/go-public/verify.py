#!/usr/bin/env python3
"""verify go-public: each node lists its public alias among its public IP candidates.

Assert each node's `ip.public_ip_candidates` includes 198.51.100.<lan-octet>.
# why: a public IPv4 candidate is what arms the nat module (mod/nat/src/module.go evaluateEnabled)
# note: the node runs in the root netns, so astral-query reaches it without `ip netns exec`
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling _lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import vmops  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", dest="vms", action="append", default=[])
    args, _ = ap.parse_known_args()
    vms = args.vms or ["node2"]

    failed = []
    for vm in vms:
        lan = vmops.peer_lan_ip(vm)
        if not lan:
            failed.append(f"{vm}: could not read its 10.77 LAN address")
            continue
        want = "198.51.100." + lan.split(".")[-1]
        raw = vmops.query(vm, "ip.public_ip_candidates")
        if want in raw:
            print(f"go-public OK: {vm} lists public candidate {want}.")
        else:
            failed.append(f"{vm}: public candidate {want} NOT among ip.public_ip_candidates")
            sys.stderr.write(f"  {vm} ip.public_ip_candidates:\n{raw or '(empty)'}\n")

    for f in failed:
        sys.stderr.write(f"go-public verify FAILED: {f}\n")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
