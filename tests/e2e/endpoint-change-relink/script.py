#!/usr/bin/env python3
"""Driver: watch node1 (B) move to a new subnet, and node2 (A) find it there.

move-subnet moves node1 from its 10.77 address to 10.78.0.<octet> --in
seconds after it ran. The driver taps every node's log, records every node's
links and the endpoints node2 resolves for node1, then polls node1's own
addresses until it holds the new one. As soon as it sees an address go, it
closes on every node the links that rode it (lib/addrmove.py says why). It
then polls node2's links to node1 and the endpoints node2 and the reflector
(C) resolve for node1 until node2 has held an outbound link to the new
address for 5 s and both resolve it, or WATCH s pass.

why no nodes.add_endpoint: what is under test is that node2 learns the new
address with no manual step. The driver never gives any node an endpoint.

The driver acts and judges nothing. It records the `endpoint_change_relink`
fact, times in seconds since the driver began, on the host's clock.
"""
import asyncio
from pathlib import Path

from lib import addrmove
from lib.sessionio import load, write_facts

# why: past the oracle's 30 s, so a late link is measured rather than missed
WATCH = 60.0
# why: past --in, the op's verifier and the session open take part of it
MARGIN = 30.0


async def main():
    nodes = load()["nodes"]
    move = addrmove.move_args(Path(__file__).parent)
    fact = await addrmove.record(nodes, move, MARGIN, WATCH)
    write_facts({"endpoint_change_relink": fact})
    w = fact["watch"] or {}
    print(f"driver: node1 changes {[(c['t'], c['added'], c['removed']) for c in fact['moved']['changes']]}; "
          f"node2 linked to {fact['targets'][-1]} at {w.get('linked_at')} s")


asyncio.run(main())
