#!/usr/bin/env python3
"""verify add-bridge: each named VM holds the bridge's address on the bridge.

Host-side check over ssh, independent of the run: `ip -o addr show dev
<name>` on each VM lists the address of --cidr.
# note: whether astrald advertises the address is the test's oracle's to judge
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
    ap.add_argument("--vm", action="append", default=[])
    ap.add_argument("--name", default="docker0")
    ap.add_argument("--cidr", default="172.17.0.1/16")
    args, _ = ap.parse_known_args()
    if not args.vm:
        sys.stderr.write("add-bridge verify FAILED: no VMs to verify\n")
        return 1

    errors = []
    for vm in args.vm:
        held = (vmops.ssh(vm, f"ip -o addr show dev {args.name}") or "").split()
        if args.cidr not in held:
            errors.append(f"{vm} does not hold {args.cidr} on {args.name}: {' '.join(held) or 'no such interface'}")
    if vmops.report_errors(errors, "add-bridge"):
        return 1
    print(f"add-bridge OK: {' '.join(args.vm)} hold {args.cidr} on {args.name}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
