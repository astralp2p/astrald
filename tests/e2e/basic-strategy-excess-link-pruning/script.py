#!/usr/bin/env python3
"""Driver: three forced basic rounds to a node with five endpoints in the book.

node2 already links node1 on its own TCP port P1 (adopt-node). The driver
gives node2 three more listeners, L2, L3 and L4 (tcp.new_ephemeral_listener),
and gives node1's book those three and D, a port nothing listens on. Each
round is `nodes.new_link` as the User on node1 with no endpoint and
`strategies=basic`: that schedules an EnsureLinkTask that forces a new link
(astrald mod/nodes/src/op_new_link.go), and BasicLinkStrategy dials every
endpoint of the book in parallel; the first link wins, and every later one
is closed with ErrExcessLink (mod/nodes/src/basic_link_strategy.go Signal).

why no endpoint: new_link with one dials exactly it (CreateLinkTask), which
is what adopt-node does, and the strategy never runs.

why the log: no op reports why a link closed, and node1's link pool logs
`closed out-link with <node2> (tcp): excess link` for each loser
(mod/nodes/src/link_pool.go AddLink), so a tap on node1 is the only witness.

The driver judges nothing. It records the `excess_prune` fact.
"""
import asyncio
import time

import astral

from lib import jsonops, linkview, ports
from lib.nodelog import LogTap
from lib.sessionio import load, write_facts

ROUNDS = 3
ROUND_TIMEOUT = 30.0
# why: a round ends when its strategy's activeDone closes, after the caller
# already has its link (basic_link_strategy.go Signal); a round started
# before that would be ignored
GAP = 2.0


def excess_lines(tap: LogTap, peer: str) -> list:
    return [e for e in tap.entries
            if e["text"].startswith(f"closed out-link with {peer} ")
            and e["text"].endswith(": excess link")]


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id1, id2 = n1["identity"], n2["identity"]
    user_token = doc["facts"]["user_token"]
    net, host2, p1 = n2["lan_endpoint"].split(":")
    l2, l3, l4, dead = ports.free_ports(4)
    live = [l2, l3, l4]

    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c:
        for p in live:
            await ports.new_ephemeral_listener(c, p)

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        for p in live + [dead]:
            await c1.nodes.add_endpoint(id2, f"{net}:{host2}:{p}",
                                        experimental=True)
        baseline1 = sorted(linkview.ids(await linkview.links(c1), id2))
        baseline2 = sorted(linkview.ids(await linkview.links(c2), id1))
        book = await jsonops.stream(
            c1, f"nodes.resolve_endpoints?identity={id2}&out=json")

    rounds = []
    async with LogTap(n1) as tap, \
            await astral.connect(n1["endpoint"], token=user_token) as cu:
        for i in range(1, ROUNDS + 1):
            t0 = time.monotonic()
            try:
                info = await cu.nodes.new_link(
                    id2, strategies="basic", experimental=True,
                    timeout=ROUND_TIMEOUT)
                r = {"id": str(info.id), "network": str(info.network),
                     "outbound": bool(info.outbound)}
            except Exception as e:  # noqa: BLE001 — every outcome is recorded
                r = {"error": f"{type(e).__name__}: {e}"}
            r["round"] = i
            r["elapsed"] = round(time.monotonic() - t0, 3)
            rounds.append(r)
            print(f"driver: round {i}: {r}")
            await asyncio.sleep(GAP)
        lines = excess_lines(tap, id2)
        tap_state = tap.state()

    print(f"driver: {len(lines)} excess-link line(s) on node1; tap {tap_state}")
    write_facts({"excess_prune": {
        "baseline1": baseline1, "baseline2": baseline2, "book": book,
        "ports": {"p1": int(p1), "live": live, "dead": dead, "host": host2},
        "rounds": rounds, "log_lines": lines, "tap": tap_state}})


asyncio.run(main())
