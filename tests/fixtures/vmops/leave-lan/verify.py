#!/usr/bin/env python3
"""verify leave-lan: <vm> withdrew its 10.77 LAN address, so it genuinely left the LAN (not merely packet-filtered).

Host-side check over ssh, independent of astral: <vm> has (1) no 10.77 LAN address and (2) no route into the 10.77 subnet.
# why: astrald keys "left the network" on the address -- it polls net.InterfaceAddrs() every 3s, advertising one tcp endpoint per IP; flushing the address fires EventNetworkAddressChanged and withdraws the endpoint (a DROP or carrier-down leaves the address and is invisible to that monitor)
# why: assert address/route, not a TCP probe error code -- a LAN connect falls through to the WAN NAT and times out rather than returning ENETUNREACH
# note: astrald re-linking over Tor is asserted separately by link-over-tor
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
    ap.add_argument("--vm", default="node2")      # node that left the LAN
    ap.add_argument("--peer", default="node1")    # node it can no longer reach
    args, _ = ap.parse_known_args()

    # note: leaver holds no 10.77 LAN address -- the thing astrald keys on
    lan_ip = astralapi.peer_lan_ip(args.vm)
    # note: and no route into the 10.77 subnet -- the connected route went with the address
    lan_routes = [ln for ln in (astralapi.ssh(args.vm, "ip -o route show") or "").splitlines()
                  if "10.77." in ln]

    if lan_ip:
        sys.stderr.write(f"leave-lan verify FAILED: {args.vm} still holds a LAN address "
                         f"({lan_ip}) -- it has not left the 10.77 LAN.\n")
        return 1
    if lan_routes:
        sys.stderr.write(f"leave-lan verify FAILED: {args.vm} still has a route into the "
                         "10.77 LAN:\n  " + "\n  ".join(lan_routes) + "\n")
        return 1

    print(f"leave-lan OK: {args.vm} withdrew its 10.77 LAN address and route -- it has left "
          f"the LAN (astrald re-links to {args.peer} over Tor; asserted by link-over-tor).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
