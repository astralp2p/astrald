#!/usr/bin/env python3
"""Driver: basic rounds whose book holds dead kcp and gw rows.

A dead kcp row is a kcp endpoint at node2's address on a UDP port nothing
listens on: the kcp dial succeeds at once (UDP is connectionless) and the
Noise handshake that follows never gets an answer. A dead gw row names a
stranger gateway (lib/strangers.py) whose only row is a dead kcp row, so its
dial nests a round for the gateway that cannot link.

Three phases, each `nodes.new_link` as the User on node1 with
`strategies=basic` and no endpoint, timed on the host's clock:

- beside: one dead kcp row next to node2's live tcp row;
- stranger: a stranger whose only row is one dead kcp row;
- crowd: node2 with 10 dead rows (6 kcp, 4 gw) next to its live tcp row.

The driver judges nothing. It records the `dead_rows` fact.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts
from lib.strangers import stranger

# why ports under 1024: no unprivileged process binds them, so nothing
# answers there on a test host or a guest
DEAD_PORTS = range(901, 921)


async def round_to(cu, target: str, timeout: float) -> dict:
    """One basic round to target: its link id and network, or its error."""
    start = time.time()
    try:
        info = await cu.nodes.new_link(target, strategies="basic",
                                       experimental=True, timeout=timeout)
        r = {"id": str(info.id), "network": str(info.network)}
    except Exception as e:  # noqa: BLE001 — every outcome is recorded
        r = {"error": f"{type(e).__name__}: {e}"}
    r["elapsed"] = round(time.time() - start, 3)
    return r


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    id2 = n2["identity"]
    ip2 = n2["lan_endpoint"].split(":")[1]
    dead = iter(f"kcp:{ip2}:{p}" for p in DEAD_PORTS)
    lone = stranger()
    gateways = [stranger() for _ in range(4)]
    facts = {"ip": ip2, "stranger": lone, "gateways": gateways, "rows": {}}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n1["endpoint"],
                                 token=doc["facts"]["user_token"]) as cu:
        async def add(identity: str, endpoint: str):
            await c1.nodes.add_endpoint(identity, endpoint, experimental=True)
            facts["rows"].setdefault(identity, []).append(endpoint)

        await add(id2, next(dead))
        facts["beside"] = await round_to(cu, id2, 20)
        print(f"driver: beside: {facts['beside']}")

        await add(lone, next(dead))
        facts["stranger_round"] = await round_to(cu, lone, 30)
        print(f"driver: stranger: {facts['stranger_round']}")

        for _ in range(5):
            await add(id2, next(dead))
        for g in gateways:
            await add(g, next(dead))
            await add(id2, f"gw:{g}:{id2}")
        facts["crowd"] = await round_to(cu, id2, 60)
        print(f"driver: crowd: {facts['crowd']}")

    write_facts({"dead_rows": facts})


asyncio.run(main())
