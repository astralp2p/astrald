#!/usr/bin/env python3
"""Driver: read what node2 advertises with a container bridge up, on node2 and on node1 after a fresh link.

add-bridge gave node2 the bridge docker0 holding 172.17.0.1/16, and the
manifest gives node2 the manual public endpoint 203.0.113.5:40000. The driver
waits SETTLE s, then reads node2's interface addresses (`ip.local_addrs`, every
non-loopback address, the bridge's and the link-local ones included) and the
endpoints node2 resolves for itself. node1 then closes its links to node2 and
links again, so the new link is node1's only link to node2 and node1 pulls
node2's endpoints from node2 (astral-docs protocols/nodes/README.md, Endpoint
exchange in the local swarm). The driver polls node1's rows for node2 until
they hold the public endpoint and node2's LAN tcp endpoint, written after the
new link, or ROWS_WAIT s pass. Last it reads node1's links with both ends.

why the TTL dates a row: node1 stores an endpoint with the TTL node2 resolves
for it and answers the time left (mod/nodes/src/db_endpoint_resolver.go), so
a row whose time left is at least node2's TTL less the time since the new
link was written by the pull over it.

The driver acts and judges nothing. It records the `advertised_clean` fact,
times in seconds since the driver began, on the host's clock.
"""
import asyncio
import time

import astral

from lib.sessionio import load, write_facts

# why: the ip module polls the interface addresses every 3 s, and an address
# change starts the 5 s announce wait (astral-docs protocols/nodes/README.md,
# Announcing a change)
SETTLE = 10.0
LINK_WAIT = 60.0
ROWS_WAIT = 30.0
POLL = 1.0
PUBLIC = "tcp:203.0.113.5:40000"
# why: a poll lists the new link up to POLL s after node1 made it, and a TTL
# counts whole seconds
SLACK = 5.0


def text(e) -> str | None:
    return None if e is None else f"{e.network()}:{e.address()}"


async def rows(c, identity: str) -> dict:
    """{`<network>:<address>`: TTL in s or None} that c's node resolves for identity."""
    return {text(e.endpoint): e.ttl
            for e in await c.nodes.resolve_endpoints(identity, experimental=True)}


async def links_to(c, peer: str) -> list:
    """c's node's links to peer, with both endpoints."""
    return [{"id": str(i.id), "network": str(i.network), "outbound": bool(i.outbound),
             "local_endpoint": text(i.local_endpoint),
             "remote_endpoint": text(i.remote_endpoint)}
            for i in await c.nodes.links(experimental=True)
            if str(i.remote_identity) == peer]


async def relink(c, peer: str, clock) -> dict:
    """Close every link of c's node to peer, link again, and when the new link showed."""
    old = await links_to(c, peer)
    for link in old:
        await c.nodes.close_link(link["id"], experimental=True)
    closed_at = clock()
    error = None
    try:
        await c.nodes.new_link(peer, experimental=True, timeout=LINK_WAIT)
    except astral.errors.AstralError as e:
        error = f"{type(e).__name__}: {e}"
    old_ids = {link["id"] for link in old}
    while clock() < closed_at + LINK_WAIT:
        new = [r for r in await links_to(c, peer) if r["id"] not in old_ids]
        if new:
            return {"closed": sorted(old_ids), "closed_at": closed_at,
                    "linked_at": clock(), "new": new, "error": error}
        await asyncio.sleep(POLL)
    return {"closed": sorted(old_ids), "closed_at": closed_at,
            "linked_at": None, "new": [], "error": error}


def fresh(held: dict, own: dict, key: str, since: float) -> bool:
    """Whether node1 holds key with a time left the pull after the new link wrote."""
    ttl, left = own.get(key), held.get(key)
    if key not in held or ttl is None:
        return key in held
    return left is not None and left >= ttl - since - SLACK


async def held_rows(c, peer: str, own: dict, lan: str, linked_at: float, clock) -> dict:
    """node1's rows for peer once they hold the public and the LAN tcp row fresh, or at ROWS_WAIT."""
    while True:
        held = await rows(c, peer)
        since = clock() - linked_at
        done = all(fresh(held, own, k, since) for k in (PUBLIC, lan))
        if done or since >= ROWS_WAIT:
            return {"rows": held, "fresh": done, "at": clock()}
        await asyncio.sleep(POLL)


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    peer, lan = n2["identity"], n2["lan_endpoint"]
    t0 = time.monotonic()
    clock = lambda: round(time.monotonic() - t0, 2)  # noqa: E731

    await asyncio.sleep(SETTLE)
    async with await astral.connect(n2["endpoint"], token=n2["token"]) as c2, \
            await astral.connect(n1["endpoint"], token=n1["token"]) as c1:
        addrs = sorted(str(a) for a in await c2.ip.local_addrs())
        own = await rows(c2, "localnode")
        linked = await relink(c1, peer, clock)
        held = None
        if linked["linked_at"] is not None:
            held = await held_rows(c1, peer, own, lan, linked["linked_at"], clock)
        links = await links_to(c1, peer)

    write_facts({"advertised_clean": {
        "lan_endpoint": lan, "local_addrs": addrs, "own": own,
        "relink": linked, "node1": held, "links": links}})
    print(f"driver: node2 holds {addrs}; advertises {sorted(own)}; node1 relinked at "
          f"{linked['linked_at']} s and holds {sorted((held or {}).get('rows', {}))}")


asyncio.run(main())
