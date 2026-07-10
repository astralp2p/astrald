#!/usr/bin/env python3
"""verify punch-nat: both NAT'd peers hold a direct kcp link to each other -- the hole-punch
completed and was promoted to a real link -- and NOT a direct/LAN tcp link (the NAT was
genuinely entered).

A kcp link is the unique signal of a completed+promoted punch: only NATLinkStrategy dials
kcp, and kcp endpoints are never advertised for an ordinary peer dial. Assert on
Network+RemoteIdentity, NOT the endpoint address (the passive/inbound side has swapped
endpoints). Negatives: no tcp link to the sibling, and none at a 10.77 LAN address (the only
tcp links present should be to the reflector at 198.51.100.<refl-oct>).

astrald is in netns "priv" on both peers -> astral-query runs inside the netns (it defaults
to tcp:127.0.0.1:8625, which is netns-local). Uses the Go CLI over ssh, not the astral-py
WS client (the WS port is netns-local too).
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling tasks/_lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import astralapi  # noqa: E402


def node_id(vm):
    """The node's own identity hex via apphost.whoami (inside its netns)."""
    return astralapi.identity_of(astralapi.parse_cli(astralapi.ssh(
        vm, "ip netns exec priv astral-query apphost.whoami -out json") or ""))


def links(vm):
    return astralapi.parse_cli(
        astralapi.ssh(vm, "ip netns exec priv astral-query nodes.links -out json") or "")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", default="node1")      # initiator
    ap.add_argument("--peer", default="node2")    # target
    args, _ = ap.parse_known_args()
    peers = [args.vm, args.peer]
    ids = {p: node_id(p) for p in peers}

    failed = []
    for p in peers:
        sib = args.peer if p == args.vm else args.vm
        sib_id = ids.get(sib, "")
        if not sib_id:
            failed.append(f"{p}: could not resolve sibling {sib} identity")
            continue
        objs = links(p)
        kcp = astralapi.kcp_links(objs)                 # [(RemoteIdentity, endpoint)]
        tcp = astralapi.links_by_network(objs, "tcp")
        # positive: a direct kcp link to the sibling (the promoted punch)
        if not any(rid == sib_id for rid, _ in kcp):
            failed.append(f"{p}: no kcp link to {sib} -- punch not promoted (kcp={kcp})")
            sys.stderr.write(f"  {p} tcp links: {tcp}\n")
            continue
        # negative: the sibling must be reachable ONLY via the punch, never a direct tcp link
        if any(rid == sib_id for rid, _ in tcp):
            failed.append(f"{p}: has a direct tcp link to {sib} -- not a NAT traversal")
            continue
        # negative: no LAN (10.77) tcp link at all -- the NAT must be genuinely entered
        if any("10.77." in str(addr) for _rid, addr in tcp):
            failed.append(f"{p}: has a 10.77 LAN tcp link -- NAT not genuinely entered (tcp={tcp})")
            continue
        print(f"punch-nat OK: {p} holds a direct kcp link to {sib} (no direct/LAN tcp link).")

    if failed:
        for f in failed:
            sys.stderr.write(f"punch-nat verify FAILED: {f}\n")
        return 1
    print(f"punch-nat verified: direct kcp link on both peers ({', '.join(peers)})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
