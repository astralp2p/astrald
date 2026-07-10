#!/usr/bin/env python3
"""verify link-over-tor: node1 holds a live link to the peer over Tor.

# why: assert Network=="tor" on nodes.links, not the .onion endpoint -- an inbound
#      tor link has no remote onion, and node2 is the only sibling.
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
    ap.add_argument("--vm", default="node1")      # operator; records tor.json
    ap.add_argument("--peer", default="node2")    # node that left the LAN
    args, _ = ap.parse_known_args()

    tor = astralapi.home_json(args.vm, "tor.json")        # agent record: peer_onion, link_network
    net = str(tor.get("link_network", ""))
    onion = str(tor.get("peer_onion", ""))

    # decisive: an actual link over Tor from node1 to the peer
    with astralapi.connect(args.vm) as node:
        links = astralapi.tor_links(node.call("nodes.links"))

    notes = []
    if net != "tor":
        notes.append(f"agent recorded link_network={net!r} (expected 'tor')")
    if not onion:
        notes.append("agent recorded no peer_onion")

    if links:
        ep = links[0][1] or "(inbound, no remote onion)"
        print(f"link-over-tor OK: {args.vm} holds a link to {args.peer} over Tor (endpoint {ep}).")
        for n in notes:
            sys.stderr.write(f"  note: {n}\n")
        return 0

    sys.stderr.write(f"link-over-tor verify FAILED: {args.vm} has no link to {args.peer} over Tor.\n")
    for n in notes:
        sys.stderr.write(f"  note: {n}\n")
    sys.stderr.write(f"  nodes.links:\n{astralapi.ssh(args.vm, 'astral-query nodes.links -out json')}\n")
    return 1


if __name__ == "__main__":
    sys.exit(main())
