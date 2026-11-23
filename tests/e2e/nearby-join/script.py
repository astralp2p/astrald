#!/usr/bin/env python3
"""Driver: a fresh reflector with a manager joins node1's swarm with user.join alone.

The driver acts and judges nothing. It appoints a manager on the fresh
reflector with an admin_network grant, so the manager may read the endpoints
the reflector resolves for node1. It reads the reflector's state and those
endpoints, then as the manager calls user.join with node1's identity and
records the answer and how long it took. Afterwards it reads the reflector's
user.info and node1's stored nearby mode.

why nothing on node1 but a read: what is under test is that the join needs
no manual step on the swarm node; node1 broadcasts in Stealth, as an owned
node does by default.

why the endpoints before the join: they show that the reflector had no route
to node1 of its own, so a successful join found node1 through nearby during
the call.

why wait out a stale entry first: nearby-adopt restarts every VM as it
pushes astrald, and a reflector that is up before node1's claim caches node1's
Visible status. That verified entry resolves node1's LAN endpoints for its
whole life, 5 min, and node1's later Stealth statuses from the same source are
dropped meanwhile (astral-docs protocols/nearby/README.md, steps 4 and 7). The
first netsim pass (2026-11-23) met exactly that. The driver waits until the
reflector resolves nothing for node1 and records the first reading as
endpoints_stale.
"""
import asyncio
import time

import astral
from astral.errors import AstralError

from lib import jsonops, nearby
from lib.sessionio import load, write_facts

# why admin_network: nodes.resolve_endpoints asks for that grant
# (manager-setup appoints its manager the same way).
MANAGER = {"Type": "mod.sys.register_manager_request",
           "Object": {"Name": "manager",
                      "GrantPermits": [{"Action": "mod.auth.admin_network_action"}]}}
# why: the route wait is 3 s, and the admission is a round trip to node1 and
# back to the reflector's user.accept_membership
JOIN_TIMEOUT = 60
# why 310 s: a nearby entry lives 5 min (mod/nearby/src/module.go statusTTL),
# plus slack for a TTL counted in whole seconds and a guest stall
STALE_WAIT = 310.0
# why: an entry's TTL says when it ends, so a poll more often than this buys nothing
STALE_POLL = 5.0


def failure(e: Exception) -> dict:
    return {"error": f"{type(e).__name__}: {e}"}


async def resolved(c, node: str):
    """(sorted `<network>:<address>` list, longest TTL in s or None) c's node resolves for node, or (failure, None)."""
    try:
        eps = await c.nodes.resolve_endpoints(node, experimental=True)
    except AstralError as e:
        return failure(e), None
    ttls = [e.ttl for e in eps if e.ttl is not None]
    return sorted(f"{e.endpoint.network()}:{e.endpoint.address()}" for e in eps), max(ttls, default=None)


async def endpoints_without_stale(c, node: str) -> dict:
    """Wait until c's node resolves no endpoint for node, at most STALE_WAIT s.

    Returns {"before": the last reading, "stale": the first non-empty reading or None,
    "waited": seconds spent}.
    """
    t0 = time.monotonic()
    stale = None
    while True:
        eps, ttl = await resolved(c, node)
        waited = round(time.monotonic() - t0, 2)
        if eps == [] or isinstance(eps, dict) or waited >= STALE_WAIT:
            return {"before": eps, "stale": stale, "waited": waited}
        if stale is None:
            stale = eps
        pause = STALE_POLL if ttl is None else min(max(ttl + 1, 1), STALE_POLL * 12)
        await asyncio.sleep(min(pause, max(STALE_WAIT - waited, 1)))


async def main():
    n = load()["nodes"]
    n1, r = n["node1"], n["reflector"]
    facts = {}

    async with await astral.connect(r["endpoint"], token=r["token"]) as c:
        facts["fresh"] = (await jsonops.value(c, "user.info?out=json"))["State"]
        manager = await jsonops.value(c, "sys.register_manager?in=json&out=json", MANAGER)
        facts["unclaimed"] = (await jsonops.value(c, "user.info?out=json"))["State"]

    async with await astral.connect(r["endpoint"], token=manager["Token"]) as m:
        seen = await endpoints_without_stale(m, n1["identity"])
        facts["endpoints_before"] = seen["before"]
        facts["endpoints_stale"] = seen["stale"]
        facts["stale_wait_seconds"] = seen["waited"]
        t0 = time.monotonic()
        try:
            signed = await m.user.join(n1["identity"], timeout=JOIN_TIMEOUT)
            facts["joined"] = {"issuer": str(signed.contract.issuer),
                               "subject": str(signed.contract.subject)}
        except AstralError as e:
            facts["joined"] = failure(e)
        facts["join_seconds"] = round(time.monotonic() - t0, 2)

    async with await astral.connect(r["endpoint"], token=r["token"]) as c:
        info = await c.user.info()
        facts["after"] = {"state": info.state, "user": str(info.user_id)}

    async with await astral.connect(n1["endpoint"], token=n1["token"]) as c:
        facts["node1_mode"] = await nearby.stored_mode(c)

    write_facts({"nearby_join": facts})
    print(f"driver: reflector {facts['fresh']} → {facts['unclaimed']}; endpoints of "
          f"node1 before {facts['endpoints_before']}; user.join answered "
          f"{facts['joined']} in {facts['join_seconds']}s; now {facts['after']}")


asyncio.run(main())
