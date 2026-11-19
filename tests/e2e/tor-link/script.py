#!/usr/bin/env python3
"""Driver: query node2 over the network change, then hold the swarm link over Tor.

Env-blind: the endpoints come from session.json, so this is the same shape as
every node-env driver. The steps before it took node2 off the LAN, so the only
mutual transport left is Tor. No step closes node1's tcp link to node2: the
LAN path black-holes, and only node1's keepalive can close that link.

1. Asks node2 for `user.info` from node1, at once and again after every
   failure, until node2 answers, while a watcher polls node1's links to node2
   every second and records when the last tcp link left. The first answer is
   the moment queries reach node2 again after the change; queries in the
   keepalive's detection window may fail (A1G2 option a).
2. Holds the swarm link over Tor: when node1 lists no tor link to node2 yet,
   kicks a tor link attempt off and watches the link table.

why this waits instead of trusting the call: astrald's Tor strategy signals
done after its quick timeout and keeps trying in the background for up to six
minutes more — "TorLinkStrategy tries to connect with retries. After
quickTimeout it signals Done but continues running in background", with a 60 s
SignalTimeout against a 360 s BackgroundTimeout (mod/nodes/src/loader.go).
`new_link` answering "link not produced" therefore means "not yet", not "no",
and a driver that took it as final failed a test the daemon was still busy
passing. The question this test asks is whether the swarm re-forms over Tor,
so it asks that: kick the attempt off, then watch the link table.

The driver judges nothing. It records the `tor_link_networks` and
`first_answer` facts, times in seconds since the driver started.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts

LINK_DEADLINE = 300.0     # inside the manifest's 600 s, outside astrald's 360 s
POLL = 5.0
QUERY = 60.0              # one query: the routing cap of a query to a peer
PAUSE = 0.5               # between a failed query and the next one
WATCH = 1.0


async def networks_to(c, peer: str) -> list:
    """Networks node1 currently holds a link to `peer` over."""
    links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


async def watch_tcp(c, peer: str, t0: float, seen: dict) -> None:
    """Records in seen when node1 last listed a tcp link to peer and when it first listed none."""
    while True:
        try:
            nets = await networks_to(c, peer)
        except Exception:  # noqa: BLE001 — a missed poll is not an observation
            nets = None
        now = round(time.monotonic() - t0, 2)
        if nets is not None:
            seen.setdefault("at_start", nets)
            if "tcp" in nets:
                seen["tcp_last_seen"] = now
                seen.pop("tcp_gone_at", None)
            else:
                seen.setdefault("tcp_gone_at", now)
        await asyncio.sleep(WATCH)


async def first_answer(c, peer: str, t0: float) -> dict:
    """Asks peer for user.info until it answers; when, since t0, and the failures before."""
    failures = []
    deadline = t0 + LINK_DEADLINE
    while time.monotonic() < deadline:
        sent = time.monotonic()
        try:
            await c.call("user.info", target=peer, timeout=QUERY)
        except Exception as e:  # noqa: BLE001 — how each query failed is the observation
            failures.append({"sent_at": round(sent - t0, 2),
                             "ended_at": round(time.monotonic() - t0, 2),
                             "how": f"{type(e).__name__}: {e}"})
            await asyncio.sleep(PAUSE)
            continue
        return {"after": round(time.monotonic() - t0, 2),
                "sent_at": round(sent - t0, 2),
                "networks": await networks_to(c, peer),
                "failures": failures}
    return {"after": None, "sent_at": None, "networks": None,
            "failures": failures}


async def hold_tor(c, peer: str) -> tuple:
    """Node1's networks to peer once a tor link is up or LINK_DEADLINE passed, and the wait."""
    start = time.monotonic()
    networks = await networks_to(c, peer)
    if "tor" in networks:
        return networks, 0.0
    try:
        await c.nodes.new_link(peer, strategies="tor",
                               experimental=True, timeout=LINK_DEADLINE)
    except astral.errors.RemoteError as e:
        # the strategy gave up early and is still working; say so and wait
        print(f"driver: new_link returned early ({e}); "
              "the tor strategy keeps trying in the background")

    deadline = start + LINK_DEADLINE
    networks = await networks_to(c, peer)
    while "tor" not in networks and time.monotonic() < deadline:
        await asyncio.sleep(POLL)
        networks = await networks_to(c, peer)
    return networks, round(time.monotonic() - start, 2)


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    peer = n2["identity"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c:
        t0 = time.monotonic()
        seen: dict = {}
        watcher = asyncio.create_task(watch_tcp(c, peer, t0, seen))
        try:
            answer = await first_answer(c, peer, t0)
        finally:
            watcher.cancel()
        answer["tcp"] = seen
        networks, waited = await hold_tor(c, peer)

    write_facts({"tor_link_networks": networks, "first_answer": answer})
    print(f"driver: node2 first answered {answer['after']} s after the driver "
          f"started, over {answer['networks']}, after "
          f"{len(answer['failures'])} failed queries; tcp {seen}")
    if "tor" not in networks:
        raise SystemExit(
            f"driver: no tor link to node2 after {waited:.0f}s; "
            f"node1 holds {networks or 'no links'} to it")
    print(f"driver: node1 linked to node2 over {networks}")


asyncio.run(main())
