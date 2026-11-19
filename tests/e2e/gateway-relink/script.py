#!/usr/bin/env python3
"""Driver: close the gw link to a node behind NAT, and watch node1's link task restore it.

gateway-relay left node2 behind NAT, registered with the reflector as its
gateway, node1 holding node2's `gw:<reflector>:<node2>` row and a gw link to
node2. The driver closes every link node1 holds to node2 and then only
watches: it polls node1's links to node2 every POLL s until an outbound gw
link stands for LINKED_FOR s, or WATCH s pass, and taps node1's log for its
link-task lines about node2. It calls no `nodes.new_link`.

The driver acts and judges nothing. It records the `gateway_relink` fact,
times in seconds since the driver began, on the host's clock.
"""
import asyncio
import time

import astral

from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

POLL = 1.0
WATCH = 180.0
LINKED_FOR = 5.0
NEEDLES = ("is broken", "trying to reconnect", "restored after", "established")


async def links_to(c, peer: str) -> list:
    return [{"id": str(l.id), "network": str(l.network), "outbound": bool(l.outbound)}
            for l in await c.nodes.links(experimental=True)
            if str(l.remote_identity) == peer]


async def main():
    nodes = load()["nodes"]
    n1, n2 = nodes["node1"], nodes["node2"]
    id2 = n2["identity"]
    t0 = time.monotonic()
    clock = lambda: round(time.monotonic() - t0, 1)  # noqa: E731
    timeline = []

    async with LogTap(n1) as tap, \
            await astral.connect(n1["endpoint"], token=n1["token"]) as c1:
        before = await links_to(c1, id2)
        for link in before:
            await c1.nodes.close_link(link["id"], experimental=True)
        closed_at = clock()
        back_at = None
        while clock() < WATCH:
            now = await links_to(c1, id2)
            if not timeline or timeline[-1]["links"] != now:
                timeline.append({"t": clock(), "links": now})
            fresh = [l for l in now if l["network"] == "gw" and l["outbound"]
                     and l["id"] not in {b["id"] for b in before}]
            if fresh and back_at is None:
                back_at = clock()
            if back_at is not None and clock() >= back_at + LINKED_FOR:
                break
            await asyncio.sleep(POLL)
        log = [e for e in tap.find(*NEEDLES) if id2 in e["text"]]
        state = tap.state()

    write_facts({"gateway_relink": {"before": before, "closed_at": closed_at,
                                    "back_at": back_at, "timeline": timeline,
                                    "log": log, "tap": state}})
    print(f"driver: closed {len(before)} link(s) to node2 at {closed_at} s; "
          f"an outbound gw link {'came back at ' + str(back_at) + ' s' if back_at else 'never came back'}")


asyncio.run(main())
