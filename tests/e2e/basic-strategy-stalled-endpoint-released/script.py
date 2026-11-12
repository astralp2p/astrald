#!/usr/bin/env python3
"""Driver: a forced basic round whose book holds an endpoint that stalls.

The driver serves a bait on the host (lib/bait.py): a TCP listener that
accepts, reads and discards, and never writes. node1's book gets the bait as
one more endpoint of node2, beside node2's own live ones (adopt-node). Two
rounds of `nodes.new_link` as the User on node1, with `strategies=basic` and
no endpoint, 3 s apart: BasicLinkStrategy dials every tcp endpoint in
parallel, so each round both links node2 and opens a Noise handshake to the
bait that never gets an answer.

why it matters: the round answers only after every worker has returned
(astrald mod/nodes/src/basic_link_strategy.go Signal, wg.Wait), so a worker
stuck on the bait holds the winner back. The winner's cancel() is what
releases it: noise.HandshakeOutbound closes the connection when its context
ends (mod/nodes/src/noise/handshake.go). brontide's handshake read timeout
is declared and never used (brontide/noise.go), so nothing else does.

The bait stays up until node1 has closed every connection it opened, or for
10 s after the last round. The driver judges nothing. It records the
`stalled_endpoint` fact, every time on the host's clock.
"""
import asyncio
import time

import astral

from lib import linkview
from lib.bait import Bait
from lib.sessionio import load, write_facts

ROUNDS = 2
ROUND_TIMEOUT = 20.0
GAP = 3.0
LINGER = 10.0


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    user_token = doc["facts"]["user_token"]

    async with Bait() as bait:
        endpoint = f"tcp:127.0.0.1:{bait.port}"
        async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
                await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
            await c1.nodes.add_endpoint(id2, endpoint, experimental=True)
            baseline1 = sorted(linkview.ids(await linkview.links(c1), id2))
            baseline2 = sorted(linkview.ids(await linkview.links(c2), id1))

        rounds = []
        async with await astral.connect(n1["endpoint"], token=user_token) as cu:
            for i in range(1, ROUNDS + 1):
                start = time.time()
                try:
                    info = await cu.nodes.new_link(
                        id2, strategies="basic", experimental=True,
                        timeout=ROUND_TIMEOUT)
                    r = {"id": str(info.id), "network": str(info.network)}
                except Exception as e:  # noqa: BLE001 — every outcome is recorded
                    r = {"error": f"{type(e).__name__}: {e}"}
                end = time.time()
                r.update(round=i, start=start, end=end,
                         elapsed=round(end - start, 3))
                rounds.append(r)
                print(f"driver: round {i}: {r}")
                await asyncio.sleep(GAP)

        deadline = rounds[-1]["end"] + LINGER
        while bait.open() and time.time() < deadline:
            await asyncio.sleep(0.1)
        conns = bait.table()
        still_open = bait.open()

    print(f"driver: bait {endpoint}: {conns}; {still_open} still open")
    write_facts({"stalled_endpoint": {
        "bait": endpoint, "baseline1": baseline1, "baseline2": baseline2,
        "rounds": rounds, "conns": conns, "still_open": still_open}})


asyncio.run(main())
