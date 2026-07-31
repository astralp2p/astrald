#!/usr/bin/env python3
"""verify add-reflector: each NAT'd peer learned its public endpoint by reflection, arming astrald's nat module.

Assert each peer's `ip.public_ip_candidates` includes its TEST-NET alias 198.51.100.<lan-octet>.
# why: that alias present == nat enabled (evaluateEnabled: enabled default on AND len(PublicIPCandidates())>0, mod/nat/src/module.go)
# note: peer astrald runs in netns "priv" (enter-nat); its WS apphost port is netns-local, so query via astral-query over the apphost unix socket, not the astral-py client
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
    ap.add_argument("--reflector", default="reflector")     # note: accepted for run.sh argv parity, unused here
    ap.add_argument("--vm", dest="vms", action="append", default=[])
    args, _ = ap.parse_known_args()
    peers = args.vms or ["node1", "node2"]

    failed = []
    for p in peers:
        lan = astralapi.peer_lan_ip(p)
        if not lan:
            failed.append(f"{p}: could not read its 10.77 LAN address")
            continue
        want = "198.51.100." + lan.split(".")[-1]           # peer's public TEST-NET alias
        # why: astral-query defaults to netns-local tcp:127.0.0.1:8625, so run it inside netns "priv"
        # note: local introspection op is ungated, no token needed
        raw = astralapi.ssh(p, "ip netns exec priv astral-query ip.public_ip_candidates -out json") or ""
        if want in raw:
            print(f"add-reflector OK: {p} nat armed -- public candidate {want} present.")
        else:
            failed.append(f"{p}: public candidate {want} NOT among ip.public_ip_candidates")
            sys.stderr.write(f"  {p} ip.public_ip_candidates:\n{raw or '(empty)'}\n")

    if failed:
        for f in failed:
            sys.stderr.write(f"add-reflector verify FAILED: {f}\n")
        return 1
    print(f"add-reflector verified: nat armed on all peers ({', '.join(peers)})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
