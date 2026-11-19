#!/usr/bin/env python3
"""verify block-tcp: tcp between the named VMs is dropped, and no tcp link joins them.

Host-side check over ssh, independent of the run: each named VM holds the
e2e_block_tcp table with every other named VM's LAN address in its peer set,
a tcp connect from each VM to another's astrald port does not complete, and no
named VM lists a tcp link to another.
# note: what astrald does over the paths left is the test's oracle's to judge
"""
import argparse
import json
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling _lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import vmops  # noqa: E402

TCP_PORT = 1791


def tcp_links(host: str, peers: set) -> list:
    """The tcp link IDs `host` holds to an identity in `peers`."""
    out = []
    for line in vmops.query(host, "nodes.links").splitlines():
        try:
            m = (json.loads(line).get("Object") or {})
        except json.JSONDecodeError:
            continue
        if isinstance(m, dict) and m.get("Network") == "tcp" and m.get("RemoteIdentity") in peers:
            out.append(str(m.get("ID")))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", action="append", default=[])
    args, _ = ap.parse_known_args()
    vms = args.vm
    errors = []
    if len(vms) < 2:
        errors.append(f"block-tcp names {vms}, not two VMs or more")
    addrs = {vm: vmops.peer_lan_ip(vm) for vm in vms}
    ids = {os.environ.get(f"ASTRAL_ID_{vm}", "") for vm in vms} - {""}
    for vm in vms:
        table = vmops.ssh(vm, "nft list table inet e2e_block_tcp") or ""
        for other in vms:
            if other == vm:
                continue
            if addrs[other] not in table:
                errors.append(f"{vm} does not drop tcp from {other} ({addrs[other]}):\n{table}")
            probe = vmops.ssh(vm, f"timeout 3 bash -c '</dev/tcp/{addrs[other]}/{TCP_PORT}' "
                                  "&& echo open || echo closed").strip()
            if probe != "closed":
                errors.append(f"{vm} still connects to {other} at {addrs[other]}:{TCP_PORT} ({probe})")
        held = tcp_links(vm, ids)
        if held:
            errors.append(f"{vm} still lists tcp links to the other VMs: {held}")
    if vmops.report_errors(errors, "block-tcp"):
        return 1
    print(f"block-tcp OK: tcp dropped among {', '.join(vms)}, and no tcp link joins them")
    return 0


if __name__ == "__main__":
    sys.exit(main())
