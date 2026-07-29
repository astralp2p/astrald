#!/usr/bin/env python3
"""verify punch-nat: both NAT'd peers hold a direct kcp link to each other and no direct/LAN
tcp link.

# why: a kcp link is the unique signal of a completed+promoted punch -- only NATLinkStrategy
#      dials kcp, never advertised for an ordinary peer dial.
# why: assert Network+RemoteIdentity, not the endpoint -- the passive/inbound side has swapped
#      endpoints.
# note: negatives prove the NAT was entered -- no tcp link to the sibling, none at a 10.77 LAN
#       address (only tcp links present go to the reflector at 198.51.100.<refl-oct>).
# note: astrald runs in netns "priv" -> astral-query runs inside the netns over the Go CLI,
#       not the astral-py WS client; both defaults (tcp:127.0.0.1:8625, WS port) are netns-local.
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach sibling tasks/_lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import astralapi  # noqa: E402


def node_id(vm):
    """Node's own identity hex via apphost.whoami inside its netns."""
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
        # positive: a direct kcp link to the sibling -- the promoted punch
        if not any(rid == sib_id for rid, _ in kcp):
            failed.append(f"{p}: no kcp link to {sib} -- punch not promoted (kcp={kcp})")
            sys.stderr.write(f"  {p} tcp links: {tcp}\n")
            continue
        # negative: sibling reachable only via the punch, never a direct tcp link
        if any(rid == sib_id for rid, _ in tcp):
            failed.append(f"{p}: has a direct tcp link to {sib} -- not a NAT traversal")
            continue
        # negative: no LAN (10.77) tcp link -- the NAT must be genuinely entered
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
