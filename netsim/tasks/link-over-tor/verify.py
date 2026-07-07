#!/usr/bin/env python3
"""verify link-over-tor: node1 holds a live link to the peer over Tor.

Independent host-side check (does not trust the agent): nodes.links on node1 must
list a link whose Network is "tor". (We assert the transport, not the .onion
endpoint string -- an inbound tor link legitimately has no remote onion, so
requiring ".onion" would false-negative; node2 is the only sibling, so a tor link
is a tor link to node2.) Also cross-checks the agent's record.

Queries reach node1's apphost through the shared astral-py client
(tasks/_lib/astralapi.py), CLI fallback for anything it can't serve.
"""
import argparse
import os
import sys

# why: realpath crosses netsim's per-task symlink to reach the sibling tasks/_lib
sys.path.insert(0, os.path.join(
    os.path.dirname(os.path.dirname(os.path.realpath(__file__))), "_lib"))
import astralapi  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--vm", default="node1")      # the operator; records tor.json here
    ap.add_argument("--peer", default="node2")    # the node that left the LAN
    args, _ = ap.parse_known_args()

    tor = astralapi.home_json(args.vm, "tor.json")        # agent: peer_onion, link_network
    net = str(tor.get("link_network", ""))
    onion = str(tor.get("peer_onion", ""))

    # Decisive: an actual link over Tor from node1 (to the only sibling, the peer).
    with astralapi.connect(args.vm) as node:
        links = [l for l in node.links() if l.network == "tor"]

    notes = []
    if net != "tor":
        notes.append(f"agent recorded link_network={net!r} (expected 'tor')")
    if not onion:
        notes.append("agent recorded no peer_onion")

    if links:
        ep = links[0].remote_address or "(inbound, no remote onion)"
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
