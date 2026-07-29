#!/usr/bin/env python3
"""verify enable-tor: each target VM runs Tor and saved its own onion endpoint.

Host-side check, independent of run.sh: tor service active, /root/tor.json holds an onion,
and that saved onion matches what astrald advertises now (nodes.resolve_endpoints -id localnode).
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
    ap.add_argument("--vm", action="append", default=[])
    args, _ = ap.parse_known_args()
    vms = args.vm or astralapi.all_running_vms()
    if not vms:
        sys.stderr.write("enable-tor verify FAILED: no VMs to verify\n")
        return 1

    bad = False
    for vm in vms:
        tor_active = astralapi.ssh(vm, "systemctl is-active tor 2>/dev/null").strip() == "active"
        file_onion = str(astralapi.read_json(vm, "/root/tor.json").get("onion", ""))
        with astralapi.connect(vm) as node:
            live = astralapi.resolve_onion(node.call("nodes.resolve_endpoints", {"id": "localnode"}))

        errs = []
        if not tor_active:
            errs.append("the tor service is not active")
        if not file_onion:
            errs.append("no onion in /root/tor.json")
        if not live:
            errs.append("astrald advertises no onion (resolve_endpoints -id localnode)")
        if file_onion and live and file_onion != live:
            errs.append(f"saved onion {file_onion} != live onion {live}")

        if astralapi.report_errors(errs, f"enable-tor on {vm}"):
            bad = True
        else:
            print(f"enable-tor OK: {vm} runs tor and saved its onion {file_onion} to /root/tor.json")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
