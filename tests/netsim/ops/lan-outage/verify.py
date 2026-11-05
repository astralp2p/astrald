#!/usr/bin/env python3
"""verify lan-outage: <vm> is off the LAN, its return is pending, and no VM still lists a link to it.

Host-side check over ssh, independent of the run: <vm> holds no 10.77 address,
its route into the LAN is unreachable, the e2e-lan-return timer waits, and no
running VM lists a link to <vm>'s identity.
# why the links: a link left standing would keep both link tasks satisfied, and
#   nothing would back off (leave-lan, step 3)
# note: what astrald does once <vm> is back is the test's oracle's to judge
"""
import argparse
import json
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling _lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import vmops  # noqa: E402


def linked_to(host: str, identity: str) -> list:
    """The link IDs `host` holds to `identity`."""
    out = []
    for line in vmops.query(host, "nodes.links").splitlines():
        try:
            m = (json.loads(line).get("Object") or {})
        except json.JSONDecodeError:
            continue
        if isinstance(m, dict) and m.get("RemoteIdentity") == identity:
            out.append(str(m.get("ID")))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", default="node2")
    args, _ = ap.parse_known_args()

    errors = []
    if vmops.peer_lan_ip(args.vm):
        errors.append(f"{args.vm} still holds a LAN address ({vmops.peer_lan_ip(args.vm)})")
    routes = vmops.ssh(args.vm, "ip -o route show") or ""
    if not any(ln.startswith("unreachable 10.77.") for ln in routes.splitlines()):
        errors.append(f"{args.vm} has no unreachable route into the LAN:\n{routes}")
    timer = (vmops.ssh(args.vm, "systemctl is-active e2e-lan-return.timer") or "").strip()
    if timer != "active":
        errors.append(f"{args.vm}'s return is not pending (e2e-lan-return.timer: {timer or '?'})")
    identity = os.environ.get(f"ASTRAL_ID_{args.vm}", "")
    if identity:
        for host in vmops.all_running_vms():
            if host != args.vm and linked_to(host, identity):
                errors.append(f"{host} still lists a link to {args.vm}: {linked_to(host, identity)}")
    if vmops.report_errors(errors, "lan-outage"):
        return 1
    print(f"lan-outage OK: {args.vm} is off the LAN, no VM links to it, and its return is pending")
    return 0


if __name__ == "__main__":
    sys.exit(main())
