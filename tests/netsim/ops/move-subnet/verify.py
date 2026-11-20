#!/usr/bin/env python3
"""verify move-subnet: <vm>'s move is pending, and every other VM routes each new subnet.

Host-side check over ssh, independent of the run: the e2e-move-subnet timer
waits on <vm>, <vm> still holds the address its state file names and none in
a --to subnet, and every other running VM routes each --to subnet over a NIC.
# why the routes: the lab's LAN has no router, so a VM without one would send
#   a packet for the new subnet out of its WAN default route
# note: what astrald does once <vm> moves is the test's oracle's to judge
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling _lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import vmops  # noqa: E402

STATE = "/run/e2e-move-subnet.env"


def state(vm: str) -> dict:
    """<vm>'s move-subnet state file as a dict ({} when it has none)."""
    out = {}
    for line in (vmops.ssh(vm, f"cat {STATE} 2>/dev/null") or "").splitlines():
        key, _, value = line.partition("=")
        if key:
            out[key.strip()] = value.strip()
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", default="node1")
    ap.add_argument("--to", action="append", default=[])
    args, _ = ap.parse_known_args()

    errors = []
    timer = (vmops.ssh(args.vm, "systemctl is-active e2e-move-subnet.timer") or "").strip()
    if timer != "active":
        errors.append(f"{args.vm}'s move is not pending (e2e-move-subnet.timer: {timer or '?'})")
    st = state(args.vm)
    held = (vmops.ssh(args.vm, "hostname -I") or "").split()
    if not st.get("cidr"):
        errors.append(f"{args.vm} has no {STATE}")
    elif st["cidr"].split("/")[0] not in held:
        errors.append(f"{args.vm} does not hold {st['cidr']}, as {STATE} says: {held}")
    early = [ip for ip in held if any(ip.startswith(p + ".") for p in args.to)]
    if early:
        errors.append(f"{args.vm} already holds an address in a --to subnet: {early}")
    for host in vmops.all_running_vms():
        if host == args.vm:
            continue
        for p in args.to:
            route = (vmops.ssh(host, f"ip -o route show {p}.0/24") or "").strip()
            if " dev " not in f" {route} ":
                errors.append(f"{host} has no route to {p}.0/24")
    if vmops.report_errors(errors, "move-subnet"):
        return 1
    print(f"move-subnet OK: {args.vm} holds {st['cidr']}, its move to {' '.join(args.to)} is pending, "
          "and every other VM routes the new subnets")
    return 0


if __name__ == "__main__":
    sys.exit(main())
