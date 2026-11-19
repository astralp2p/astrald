#!/usr/bin/env python3
"""Driver: a basic round to a target behind gateways that reach each other only through each other.

node1's book gets three strangers (lib/strangers.py), each with one gw row:

- the target T at gw:A:T, behind gateway A;
- gateway A at gw:B:A, reachable only through gateway B;
- gateway B at gw:A:B, reachable only through gateway A.

Dialing gw:A:T routes a query to A, which starts a round for A; that round
dials gw:B:A and starts a round for B, whose gw:A:B leads back to A while A's
round is still running. Nothing in the chain can link.

Two `nodes.new_link` rounds to T as the User on node1, `strategies=basic`,
with a caller timeout of CALLER s, timed on the host's clock, and a liveness
read of node1 after each. The driver judges nothing. It records the
`gw_cycle` fact.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts
from lib.strangers import stranger

ROUNDS = 2
CALLER = 90.0


async def main():
    doc = load()
    n1 = doc["nodes"]["node1"]
    t, a, b = stranger(), stranger(), stranger()
    rows = {t: f"gw:{a}:{t}", a: f"gw:{b}:{a}", b: f"gw:{a}:{b}"}
    rounds = []

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n1["endpoint"],
                                 token=doc["facts"]["user_token"]) as cu:
        for identity, endpoint in rows.items():
            await c1.nodes.add_endpoint(identity, endpoint, experimental=True)

        for i in range(1, ROUNDS + 1):
            start = time.time()
            try:
                info = await cu.nodes.new_link(t, strategies="basic", experimental=True,
                                               timeout=CALLER)
                r = {"id": str(info.id), "network": str(info.network)}
            except Exception as e:  # noqa: BLE001 — every outcome is recorded
                r = {"error": f"{type(e).__name__}: {e}"}
            r.update(round=i, elapsed=round(time.time() - start, 3))
            whoami = time.time()
            await c1.call("apphost.whoami", timeout=10)
            r["whoami"] = round(time.time() - whoami, 3)
            rounds.append(r)
            print(f"driver: round {i}: {r}")

        linked = sorted({str(l.remote_identity) for l in await c1.nodes.links(experimental=True)}
                        & set(rows))

    write_facts({"gw_cycle": {"target": t, "gateways": [a, b], "rows": rows,
                              "rounds": rounds, "linked": linked}})


asyncio.run(main())
