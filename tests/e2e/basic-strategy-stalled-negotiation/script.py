#!/usr/bin/env python3
"""Driver: a forced basic round whose book holds a peer that stalls after the
Noise handshake.

The driver starts the fake peer (driver/main.go) on the test host, at node1's
host_ip: an identity X with no astrald behind it, on three TCP listeners.
`stall` completes the Noise responder handshake and then sends nothing;
`live2` and `live3` complete the handshake and the mux2 negotiation and hold
the link, answering pings.

As node1, with node1's own token:

1. adds `stall` and `live2` to node1's book as endpoints of X;
2. round 1: `nodes.new_link(X, strategies=basic)`, 15 s timeout;
3. adds `live3`; round 2: the same call, 10 s timeout.

BasicLinkStrategy dials every endpoint in parallel, so each round both wins
on a live listener and leaves one worker in mux negotiation with `stall`
(astrald mod/nodes/src/link_negotiator.go NegotiateOutbound). The round
reports its winner only after every worker returns (basic_link_strategy.go
Signal, wg.Wait), and a round still running makes the next Signal a no-op.

The fake peer keeps running after the driver: the oracle reads its state and
stops it. The driver judges nothing. It records the `stalled_negotiation`
fact, every time on the host's clock.
"""
import asyncio
import time

import astral

from lib import apps, linkview
from lib.sessionio import load, write_facts

PKG = "tests/e2e/basic-strategy-stalled-negotiation/driver"
NAME = "basic-strategy-stalled-negotiation-peer"
ROUND_TIMEOUTS = (15.0, 10.0)
LINGER = 5.0


async def new_link(c, peer: str, timeout: float, i: int) -> dict:
    start = time.time()
    try:
        info = await c.nodes.new_link(peer, strategies="basic",
                                      experimental=True, timeout=timeout)
        r = {"id": str(info.id), "network": str(info.network)}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        r = {"error": f"{type(e).__name__}: {e}"}
    end = time.time()
    r.update(round=i, start=start, end=end, elapsed=round(end - start, 3),
             timeout=timeout)
    print(f"driver: round {i}: {r}")
    return r


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    host = n1["host_ip"]

    binary = apps.build_go(PKG)
    peer = apps.spawn(NAME, [binary], env={"FAKEPEER_HOST": host})
    st = peer.wait_state(lambda s: len(s.get("ports", {})) == 3, timeout=30)
    x, ports = st["identity"], st["ports"]
    endpoints = {name: f"tcp:{host}:{port}" for name, port in ports.items()}
    print(f"driver: fake peer {x[:16]}… on {endpoints}")

    rounds = []
    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        for name in ("stall", "live2"):
            await c.nodes.add_endpoint(x, endpoints[name], experimental=True)
        rounds.append(await new_link(c, x, ROUND_TIMEOUTS[0], 1))
        links1 = linkview.to(await linkview.links(c), x)
        await c.nodes.add_endpoint(x, endpoints["live3"], experimental=True)
        rounds.append(await new_link(c, x, ROUND_TIMEOUTS[1], 2))
        await asyncio.sleep(LINGER)
        links2 = linkview.to(await linkview.links(c), x)

    write_facts({"stalled_negotiation": {
        "peer": x, "app": NAME, "endpoints": endpoints, "rounds": rounds,
        "links_after_round1": links1, "links_after_round2": links2}})


asyncio.run(main())
