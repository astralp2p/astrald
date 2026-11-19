#!/usr/bin/env python3
"""Driver: leave node1 and node2's links idle and count their keepalive probes.

adopt-node leaves node1 and node2 linked siblings. This world sets tcp's and
kcp's link_idle_ping and link_ping_timeout to 2 s on both nodes, and
nodes.log_pings, so every pong a node receives logs `ping with <peer>`
(astrald mod/nodes/src/mux.go handlePing).

The driver taps both nodes' logs, waits until both ends' link ids hold still
for 5 s, then watches IDLE s: both ends' link ids every second, and every
`ping with` and `closed` line about the peer. It records the
`idle_keepalive` fact and judges nothing.
"""
import asyncio
import time
from contextlib import AsyncExitStack

import astral

from lib import linkview
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

NAMES = ("node1", "node2")
IDLE = 40.0


async def snapshot(c: dict, ids: dict) -> dict:
    """Each node's link ids to the other, sorted."""
    a, b = NAMES
    return {a: sorted(linkview.ids(await linkview.links(c[a]), ids[b])),
            b: sorted(linkview.ids(await linkview.links(c[b]), ids[a]))}


def lines(tap: LogTap, mark: int, needle: str) -> list:
    return [e for e in tap.entries[mark:] if needle in e["text"]]


async def main():
    doc = load()
    nodes = {n: doc["nodes"][n] for n in NAMES}
    ids = {n: nodes[n]["identity"] for n in NAMES}
    async with AsyncExitStack() as stack:
        c = {n: await stack.enter_async_context(
            await astral.connect(nodes[n]["endpoint"], token=nodes[n]["token"]))
            for n in NAMES}
        taps = {n: await stack.enter_async_context(LogTap(nodes[n])) for n in NAMES}

        base, settled = await linkview.settle(lambda: snapshot(c, ids), timeout=60.0)
        print(f"driver: baseline settled={settled} {base}")
        marks = {n: len(taps[n].entries) for n in NAMES}
        t0 = time.monotonic()
        snaps = []
        while time.monotonic() - t0 < IDLE:
            snaps.append(await snapshot(c, ids))
            await asyncio.sleep(1.0)
        watched = round(time.monotonic() - t0, 2)

        peer = {NAMES[0]: ids[NAMES[1]], NAMES[1]: ids[NAMES[0]]}
        pings = {n: len(lines(taps[n], marks[n], f"ping with {peer[n]}")) for n in NAMES}
        closed = {n: [e["text"] for e in lines(taps[n], marks[n], "closed")
                      if peer[n] in e["text"]] for n in NAMES}
        tap_state = {n: taps[n].state() for n in NAMES}

    write_facts({"idle_keepalive": {
        "ids": ids, "settled": settled, "baseline": base, "watched": watched,
        "snapshots": snaps, "pings": pings, "closed": closed, "taps": tap_state}})
    print(f"driver: watched {watched} s; pongs {pings}; closed {closed}")


asyncio.run(main())
