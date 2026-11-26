#!/usr/bin/env python3
"""Driver: query node2 over the network change, then hold the swarm link over Tor.

Env-blind: the endpoints come from session.json, so this is the same shape as
every node-env driver. The steps before it took node2 off the LAN, so the only
mutual transport left is Tor. No step closes node1's tcp link to node2: the
LAN path black-holes, and only node1's keepalive can close that link.

1. Asks node2 for `user.info` from node1, at once and again after every
   failure, until node2 answers, while a watcher polls node1's links to node2
   every second. The first answer is the moment queries reach node2 again
   after the change; queries in the keepalive's detection window may fail
   (A1G2 option a).
2. Holds the swarm link over Tor: when node1 lists no tor link to node2 yet,
   kicks a tor link attempt off and watches the link table.
3. Lets the watcher finish: node1's old links to node2 gone, and node1's
   endpoint table refreshed with what node2 advertises, or ENDPOINTS_WAIT s
   past the new link.

The watcher records, in the `tcp` entry of `first_answer`:

- when node1 last listed a tcp link to node2, and when it first listed none;
- the old links: node1's non-tor links to node2 at the first poll, which ran
  over the LAN; when node1 first listed none of them (`old_gone_at`);
- the new link: when node1 first listed a link to node2 that is not an old
  one (`new_at`), and its networks;
- the endpoint table: from the new link on, node2's own endpoints
  (`nodes.resolve_endpoints localnode` on node2) against node1's rows for
  node2 (`nodes.resolve_endpoints <node2>` on node1), and when every row of
  node2's was in node1's table, written after the new link (`at`).

why a row's TTL says when node1 wrote it: node1 stores an endpoint with the
TTL node2 resolves for it and answers the time left
(mod/nodes/src/db_endpoint_resolver.go), and node2 answers the full TTL each
time. A row of node1's whose time left is at least node2's TTL less the time
since the new link was written after it, by a pull over the new link; the
rows node1 held before (the join's pull, leave-lan's seeded onion) are older.

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

from lib import linkview
from lib.sessionio import load, write_facts

LINK_DEADLINE = 300.0     # inside the manifest's 780 s, outside astrald's 360 s
POLL = 5.0
QUERY = 60.0              # one query: the routing cap of a query to a peer
PAUSE = 0.5               # between a failed query and the next one
WATCH = 1.0
# why 60 s: ticket akchpFNjbO criterion 6, the endpoint table within 60 s of the new link
ENDPOINTS_WAIT = 60.0
# why: a poll lists the new link up to WATCH s after node1 made it, and a TTL counts whole seconds
ENDPOINT_SLACK = 5.0
NETWORKS = ("tcp", "kcp", "tor")


async def networks_to(c, peer: str) -> list:
    """Networks node1 currently holds a link to `peer` over."""
    links = await c.nodes.links(experimental=True)
    return sorted({l.network for l in links
                   if str(l.remote_identity) == peer})


async def endpoint_ttls(c, name: str) -> dict:
    """{`<network>:<address>`: TTL in s or None} of the tcp, kcp and tor endpoints c's node resolves for name."""
    rows = {}
    for e in await c.nodes.resolve_endpoints(name, experimental=True):
        ep = e.endpoint
        if callable(getattr(ep, "network", None)) and ep.network() in NETWORKS:
            rows[f"{ep.network()}:{ep.address()}"] = e.ttl
    return rows


def stale_rows(own: dict, held: dict, since_new: float) -> list:
    """The rows of node2's own that node1 lacks or wrote before the new link."""
    out = []
    for key, ttl in sorted(own.items()):
        if key not in held:
            out.append(f"{key} missing")
        elif ttl is not None and (held[key] is None
                                  or held[key] < ttl - since_new - ENDPOINT_SLACK):
            out.append(f"{key} ttl {held[key]} s, want >= {ttl - since_new - ENDPOINT_SLACK:.0f} s")
    return out


def watch_links(rows: list, now: float, seen: dict) -> None:
    """Records the tcp, old-link and new-link times from one poll of node1's links to node2."""
    nets = sorted({r["network"] for r in rows})
    ids = {r["id"] for r in rows}
    if "old_ids" not in seen:
        seen["at_start"] = nets
        seen["old_ids"] = sorted(r["id"] for r in rows if r["network"] != "tor")
    if "tcp" in nets:
        seen["tcp_last_seen"] = now
        seen.pop("tcp_gone_at", None)
    else:
        seen.setdefault("tcp_gone_at", now)
    if ids & set(seen["old_ids"]):
        seen.pop("old_gone_at", None)
    else:
        seen.setdefault("old_gone_at", now)
    new = [r for r in rows if r["id"] not in seen["old_ids"]]
    if new and seen.get("new_at") is None:
        seen["new_at"] = now
        seen["new_networks"] = sorted({r["network"] for r in new})


async def watch_endpoints(c1, c2, peer: str, now: float, seen: dict) -> None:
    """Records when node1's table first held node2's own rows, written after the new link."""
    ep = seen.setdefault("endpoints", {"at": None})
    if ep["at"] is not None:
        return
    own, held = await endpoint_ttls(c2, "localnode"), await endpoint_ttls(c1, peer)
    stale = stale_rows(own, held, now - seen["new_at"])
    ep.update({"own": own, "held": held, "stale": stale, "checked_at": now})
    if own and not stale:
        ep["at"] = now


def watch_done(seen: dict, now: float) -> bool:
    """Whether the watcher has seen all it waits for, or waited ENDPOINTS_WAIT s past the new link."""
    new_at = seen.get("new_at")
    if new_at is None:
        return False
    settled = (seen.get("old_gone_at") is not None
               and (seen.get("endpoints") or {}).get("at") is not None)
    return settled or now > new_at + ENDPOINTS_WAIT + ENDPOINT_SLACK


async def watch(c1, c2, peer: str, t0: float, seen: dict) -> None:
    """Polls node1's links to peer, and its endpoint table once a new link is up, every WATCH s."""
    while True:
        now = round(time.monotonic() - t0, 2)
        try:
            watch_links(linkview.to(await linkview.links(c1), peer), now, seen)
            if seen.get("new_at") is not None:
                await watch_endpoints(c1, c2, peer, now, seen)
        except Exception as e:  # noqa: BLE001 — a missed poll is not an observation
            seen["poll_errors"] = seen.get("poll_errors", 0) + 1
            seen["last_poll_error"] = f"{type(e).__name__}: {e}"
        if watch_done(seen, now):
            return
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


async def finish(watcher: asyncio.Task) -> None:
    """Waits for the watcher until it is done, or ENDPOINTS_WAIT plus slack more; then stops it."""
    try:
        await asyncio.wait_for(asyncio.shield(watcher), ENDPOINTS_WAIT + 2 * ENDPOINT_SLACK)
    except asyncio.TimeoutError:
        pass
    watcher.cancel()


async def main():
    doc = load()
    n1, n2 = doc["nodes"]["node1"], doc["nodes"]["node2"]
    peer = n2["identity"]

    async with await astral.connect(n1["endpoint"],
                                    token=doc["facts"]["user_token"]) as c, \
            await astral.connect(n1["endpoint"], token=n1["token"]) as c1, \
            await astral.connect(n2["endpoint"], token=n2["token"]) as c2:
        t0 = time.monotonic()
        seen: dict = {}
        watcher = asyncio.create_task(watch(c1, c2, peer, t0, seen))
        try:
            answer = await first_answer(c, peer, t0)
            answer["tcp"] = seen
            networks, waited = await hold_tor(c, peer)
        finally:
            await finish(watcher)

    write_facts({"tor_link_networks": networks, "first_answer": answer})
    print(f"driver: node2 first answered {answer['after']} s after the driver "
          f"started, over {answer['networks']}, after "
          f"{len(answer['failures'])} failed queries; links {seen}")
    if "tor" not in networks:
        raise SystemExit(
            f"driver: no tor link to node2 after {waited:.0f}s; "
            f"node1 holds {networks or 'no links'} to it")
    print(f"driver: node1 linked to node2 over {networks}")


asyncio.run(main())
