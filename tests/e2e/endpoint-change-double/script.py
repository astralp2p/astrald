#!/usr/bin/env python3
"""Driver: watch node1 (B) change its address twice within 5 s, and the final address reach its siblings.

endpoint-change-relink left node1 at 10.78.0.<octet>. move-subnet moves it
to 10.79.0.<octet> --in seconds after it ran, and to 10.80.0.<octet> --gap
seconds later. The driver taps every node's log, polls node1's own addresses
until it holds the final one, closing on every node the links that rode each
address it saw go (lib/addrmove.py says why), then polls node2's (A) links to
node1 and the endpoints node2 and the reflector (C) resolve for node1 until
node2 has held an outbound link to the final address for 5 s and both
resolve it, or WATCH s pass. It gives no node an endpoint.

The driver acts and judges nothing. It records the `endpoint_change_double`
fact, times in seconds since the driver began, on the host's clock.
"""
import asyncio
from pathlib import Path

from lib import addrmove
from lib.sessionio import load, write_facts

# why: past the oracle's 30 s, so a late arrival is measured rather than missed
WATCH = 60.0
# why: past --in and --gap, the op's verifier and the session open take part of --in
MARGIN = 30.0


async def main():
    nodes = load()["nodes"]
    move = addrmove.move_args(Path(__file__).parent)
    fact = await addrmove.record(nodes, move, MARGIN + move["gap"], WATCH)
    write_facts({"endpoint_change_double": fact})
    w = fact["watch"] or {}
    print(f"driver: node1 changes {[(c['t'], c['added'], c['removed']) for c in fact['moved']['changes']]}; "
          f"{fact['targets'][-1]} on node2 at {w.get('sibling_row_at')} s, on the reflector at "
          f"{w.get('witness_row_at')} s; node2 linked at {w.get('linked_at')} s")


asyncio.run(main())
